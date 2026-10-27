package rbac

// Package rbac.service.go ports yii\rbac\DbManager to Go.
//
// The four models (AuthItem, AuthItemChild, AuthAssignment, AuthRule)
// keep the same schema as the Yii2 tables, so an existing rbac dataset
// migrates over with no changes.
//
// Two things differ from the PHP port:
//
//  1. Rules are a Go registry, not serialised class instances. A rule
//     is registered by name at init() from anywhere in the codebase
//     via RegisterRbacRule, and executeRule looks it up at check time.
//     The `auth_rule` row still exists (so it shows up in the admin UI
//     and its `name` binds an item to a rule), but its `data` blob is
//     ignored — Yii2 stored a serialised PHP object there, which has
//     no Go equivalent.
//
//  2. The in-memory cache is a single process-wide struct guarded by
//     an RWMutex, loaded lazily on the first CheckAccess and rebuilt
//     wholesale on any mutating call. Yii2 uses its `Cache` component
//     (often APCu or Redis) with the same invalidate-on-write shape.

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"app/pkg/config"
	"app/pkg/core/db/activerecord"
	"app/pkg/core/rbac/model"
	"app/pkg/enum"

	"gorm.io/gorm"
)

// Item types match yii\rbac\Item::TYPE_ROLE / TYPE_PERMISSION so
// existing auth_item rows keep the same semantics.
const (
	RbacItemTypeRole       = 1
	RbacItemTypePermission = 2
)

// RbacRuleFn is the Go equivalent of a yii\rbac\Rule instance.
// Returning true means the item's constraint is satisfied for that
// user and those params; false denies the item (and short-circuits
// the recursive check the same way Yii2 does).
type RbacRuleFn func(userId uint32, item *model.AuthItem, params map[string]any) bool

var (
	rbacRulesMu sync.RWMutex
	rbacRules   = map[string]RbacRuleFn{}
)

// RegisterRbacRule wires a name → rule-function into the registry.
// Callers register from init() in a dedicated rules package so the
// map is fully populated before the first CheckAccess.
func RegisterRbacRule(name string, fn RbacRuleFn) {
	rbacRulesMu.Lock()
	defer rbacRulesMu.Unlock()
	rbacRules[name] = fn
}

// LookupRbacRule returns the registered function, or (nil, false)
// if none. CheckAccess treats a missing rule the same way Yii2 does
// when unserialize returns null — permission is DENIED. Register a
// noop rule explicitly if that isn't what you want.
func LookupRbacRule(name string) (RbacRuleFn, bool) {
	rbacRulesMu.RLock()
	defer rbacRulesMu.RUnlock()
	fn, ok := rbacRules[name]
	return fn, ok
}

// RbacManager is the runtime port of yii\rbac\DbManager. All queries
// hit MySQL through GORM; the `items` / `parents` / `rules` maps
// mirror DbManager's PHP cache and are guarded by cacheMu.
//
// The process-wide singleton `rbac` (see Rbac()) is fine for a
// single-server monolith. If you shard by pod, replace it with a
// Redis-backed cache and change loadCache / invalidate accordingly.
type RbacManager struct {
	tx *gorm.DB

	// DefaultRoles auto-grant to every user without needing an
	// auth_assignment row — matches DbManager::$defaultRoles.
	// Configure at boot via SetDefaultRoles.
	defaultRolesMu sync.RWMutex
	defaultRoles   []string

	cacheMu sync.RWMutex
	items   map[string]*model.AuthItem
	parents map[string][]string // child → []parent
	rules   map[string]*model.AuthRule
	loaded  bool

	// Per-user assignment memo, invalidated on Assign/Revoke.
	// Mirror of DbManager::$checkAccessAssignments.
	assignmentsMu    sync.Mutex
	assignmentsCache map[uint32]map[string]*model.AuthAssignment
}

var rbacSingleton = &RbacManager{
	items:            map[string]*model.AuthItem{},
	parents:          map[string][]string{},
	rules:            map[string]*model.AuthRule{},
	assignmentsCache: map[uint32]map[string]*model.AuthAssignment{},
}

// Rbac returns the process-wide manager. Middleware and services
// call Rbac().CheckAccess(...) directly; the gRPC handlers wrap
// it for admin UI operations.
func Rbac() *RbacManager { return rbacSingleton }

func (m *RbacManager) db() *gorm.DB { return activerecord.DB(m.tx) }

// SetDefaultRoles sets the list of auto-granted role names.
func (m *RbacManager) SetDefaultRoles(names ...string) {
	m.defaultRolesMu.Lock()
	defer m.defaultRolesMu.Unlock()
	m.defaultRoles = append(m.defaultRoles[:0], names...)
}

// GetDefaultRoles returns a copy of the default-role list.
func (m *RbacManager) GetDefaultRoles() []string {
	m.defaultRolesMu.RLock()
	defer m.defaultRolesMu.RUnlock()
	out := make([]string, len(m.defaultRoles))
	copy(out, m.defaultRoles)
	return out
}

// ---------- cache ----------

// loadCache builds items / parents / rules from a single scan of the
// three tables. Called on the first CheckAccess and again after any
// InvalidateCache. Cheap enough at typical RBAC sizes (<10k rows) that
// no delta-refresh is needed.
func (m *RbacManager) loadCache() {
	m.cacheMu.RLock()
	loaded := m.loaded
	m.cacheMu.RUnlock()
	if loaded {
		return
	}

	items := map[string]*model.AuthItem{}
	parents := map[string][]string{}
	rules := map[string]*model.AuthRule{}

	var rows []*model.AuthItem
	if err := m.db().Table(model.TableNameAuthItem).Find(&rows).Error; err != nil {
		config.Log.Errorf("rbac: loadCache items: %v", err)
	}
	for _, it := range rows {
		items[it.Name] = it
	}

	var children []*model.AuthItemChild
	if err := m.db().Table(model.TableNameAuthItemChild).Find(&children).Error; err != nil {
		config.Log.Errorf("rbac: loadCache item_child: %v", err)
	}
	for _, c := range children {
		parents[c.Child] = append(parents[c.Child], c.Parent)
	}

	var ruleRows []*model.AuthRule
	if err := m.db().Table(model.TableNameAuthRule).Find(&ruleRows).Error; err != nil {
		config.Log.Errorf("rbac: loadCache rules: %v", err)
	}
	for _, r := range ruleRows {
		rules[r.Name] = r
	}

	m.cacheMu.Lock()
	m.items = items
	m.parents = parents
	m.rules = rules
	m.loaded = true
	m.cacheMu.Unlock()
}

// InvalidateCache marks the item / parent / rule cache stale so the
// next CheckAccess rebuilds it. Called from every mutating method
// below — the same shape DbManager uses.
func (m *RbacManager) InvalidateCache() {
	m.cacheMu.Lock()
	m.items = map[string]*model.AuthItem{}
	m.parents = map[string][]string{}
	m.rules = map[string]*model.AuthRule{}
	m.loaded = false
	m.cacheMu.Unlock()

	m.assignmentsMu.Lock()
	m.assignmentsCache = map[uint32]map[string]*model.AuthAssignment{}
	m.assignmentsMu.Unlock()
}

// ---------- checkAccess ----------

// CheckAccess is the entry point every caller uses: middleware,
// controllers, cron jobs. userId 0 or an empty permission name
// short-circuits to false — matches DbManager::hasNoAssignments.
func (m *RbacManager) CheckAccess(userId uint32, permissionName string, params map[string]any) bool {
	if userId == 0 || permissionName == "" {
		return false
	}
	assignments := m.getAssignmentsMemo(userId)

	m.loadCache()
	return m.checkAccessFromCache(userId, permissionName, params, assignments)
}

func (m *RbacManager) getAssignmentsMemo(userId uint32) map[string]*model.AuthAssignment {
	m.assignmentsMu.Lock()
	if cached, ok := m.assignmentsCache[userId]; ok {
		m.assignmentsMu.Unlock()
		return cached
	}
	m.assignmentsMu.Unlock()

	fresh := m.GetAssignments(userId)

	m.assignmentsMu.Lock()
	m.assignmentsCache[userId] = fresh
	m.assignmentsMu.Unlock()
	return fresh
}

// checkAccessFromCache walks the parent chain in memory. Mirrors
// DbManager::checkAccessFromCache: rule first, then direct assignment
// or default-role hit, then recurse into parents.
func (m *RbacManager) checkAccessFromCache(userId uint32, itemName string, params map[string]any, assignments map[string]*model.AuthAssignment) bool {
	m.cacheMu.RLock()
	item, ok := m.items[itemName]
	parents := append([]string(nil), m.parents[itemName]...)
	m.cacheMu.RUnlock()
	if !ok {
		return false
	}
	if !m.executeRule(userId, item, params) {
		return false
	}
	if _, hit := assignments[itemName]; hit {
		return true
	}
	for _, dr := range m.GetDefaultRoles() {
		if dr == itemName {
			return true
		}
	}
	for _, parent := range parents {
		if m.checkAccessFromCache(userId, parent, params, assignments) {
			return true
		}
	}
	return false
}

// executeRule mirrors BaseManager::executeRule. No rule name → allow.
// Rule name set but not registered → deny (matches Yii2's unserialize
// returning null → executeRule returns false).
func (m *RbacManager) executeRule(userId uint32, item *model.AuthItem, params map[string]any) bool {
	name := item.GetRuleName()
	if name == "" {
		return true
	}
	fn, ok := LookupRbacRule(name)
	if !ok {
		config.Log.Warnf("rbac: rule %q referenced by item %q is not registered", name, item.Name)
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			config.Log.Errorf("rbac: rule %q panic on user=%d item=%s: %v", name, userId, item.Name, r)
		}
	}()
	return fn(userId, item, params)
}

// ---------- items (roles + permissions) ----------

// GetItem returns one auth_item by name from cache when loaded, else
// straight from the DB — same fallback DbManager uses.
func (m *RbacManager) GetItem(name string) *model.AuthItem {
	if name == "" {
		return nil
	}
	m.cacheMu.RLock()
	if m.loaded {
		item := m.items[name]
		m.cacheMu.RUnlock()
		return item
	}
	m.cacheMu.RUnlock()

	item, _ := activerecord.FindOneWhere[model.AuthItem]("`name` = ?", name)
	return item
}

// GetRole and GetPermission are typed variants that also enforce
// the item's type — a permission name passed to GetRole returns nil.
func (m *RbacManager) GetRole(name string) *model.AuthItem {
	it := m.GetItem(name)
	if it == nil || it.Type != RbacItemTypeRole {
		return nil
	}
	return it
}

func (m *RbacManager) GetPermission(name string) *model.AuthItem {
	it := m.GetItem(name)
	if it == nil || it.Type != RbacItemTypePermission {
		return nil
	}
	return it
}

// GetItems returns every item of a given type — GetRoles wraps it
// with itemType=1, GetPermissions with itemType=2 (matches DbManager).
func (m *RbacManager) GetItems(itemType int) map[string]*model.AuthItem {
	out := map[string]*model.AuthItem{}
	var rows []*model.AuthItem
	if err := m.db().Table(model.TableNameAuthItem).Where("type = ?", itemType).Find(&rows).Error; err != nil {
		config.Log.Errorf("rbac: GetItems(%d): %v", itemType, err)
		return out
	}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func (m *RbacManager) GetRoles() map[string]*model.AuthItem {
	return m.GetItems(RbacItemTypeRole)
}

func (m *RbacManager) GetPermissions() map[string]*model.AuthItem {
	return m.GetItems(RbacItemTypePermission)
}

// AddItem inserts an auth_item row. `data` is JSON-encoded when
// non-empty (Yii2 uses PHP serialize; JSON matches Go conventions
// and is human-readable in the admin UI).
func (m *RbacManager) AddItem(item *model.AuthItem) error {
	if item == nil || item.Name == "" {
		return errors.New("rbac: item requires a name")
	}
	if item.Type != RbacItemTypeRole && item.Type != RbacItemTypePermission {
		return errors.New("rbac: item type must be role (1) or permission (2)")
	}
	now := uint32(time.Now().Unix())
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	if item.UpdatedAt == 0 {
		item.UpdatedAt = now
	}
	if err := m.db().Table(model.TableNameAuthItem).Create(item).Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

// UpdateItem mirrors DbManager::updateItem including its rename-cascade
// path: when `name` changes and no FK ON UPDATE CASCADE is in effect,
// the child + assignment references must be moved by hand.
func (m *RbacManager) UpdateItem(oldName string, item *model.AuthItem) error {
	if item == nil || oldName == "" {
		return errors.New("rbac: UpdateItem needs oldName and item")
	}
	tx := m.db().Begin()
	if item.Name != oldName {
		if err := tx.Table(model.TableNameAuthItemChild).Where("parent = ?", oldName).Update("parent", item.Name).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Table(model.TableNameAuthItemChild).Where("child = ?", oldName).Update("child", item.Name).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Table(model.TableNameAuthAssignment).Where("item_name = ?", oldName).Update("item_name", item.Name).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	item.UpdatedAt = uint32(time.Now().Unix())
	err := tx.Table(model.TableNameAuthItem).Where("name = ?", oldName).Updates(map[string]any{
		"name":        item.Name,
		"description": item.Description,
		"rule_name":   item.RuleName,
		"data":        item.Data,
		"updated_at":  item.UpdatedAt,
	}).Error
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

// RemoveItem drops an item plus its child + assignment rows so the
// graph doesn't leak dangling references after the FK removes them
// (the migration marks these as CASCADE, but we clean explicitly for
// backends where cascades are off).
func (m *RbacManager) RemoveItem(name string) error {
	tx := m.db().Begin()
	if err := tx.Table(model.TableNameAuthItemChild).Where("parent = ? OR child = ?", name, name).Delete(nil).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Table(model.TableNameAuthAssignment).Where("item_name = ?", name).Delete(nil).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Table(model.TableNameAuthItem).Where("name = ?", name).Delete(nil).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

// ---------- rules ----------

// GetRule returns one auth_rule row. Since rule bodies live in the Go
// registry rather than the `data` blob, this is only useful for admin
// UI listings — CheckAccess itself uses LookupRbacRule.
func (m *RbacManager) GetRule(name string) *model.AuthRule {
	m.cacheMu.RLock()
	if m.loaded {
		r := m.rules[name]
		m.cacheMu.RUnlock()
		return r
	}
	m.cacheMu.RUnlock()
	r, _ := activerecord.FindOneWhere[model.AuthRule]("`name` = ?", name)
	return r
}

func (m *RbacManager) GetRules() map[string]*model.AuthRule {
	m.loadCache()
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	out := make(map[string]*model.AuthRule, len(m.rules))
	maps.Copy(out, m.rules)
	return out
}

func (m *RbacManager) AddRule(rule *model.AuthRule) error {
	if rule == nil || rule.Name == "" {
		return errors.New("rbac: rule requires a name")
	}
	now := uint32(time.Now().Unix())
	if rule.CreatedAt == 0 {
		rule.CreatedAt = now
	}
	if rule.UpdatedAt == 0 {
		rule.UpdatedAt = now
	}
	if err := m.db().Table(model.TableNameAuthRule).Create(rule).Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

func (m *RbacManager) UpdateRule(oldName string, rule *model.AuthRule) error {
	if rule == nil || oldName == "" {
		return errors.New("rbac: UpdateRule needs oldName and rule")
	}
	tx := m.db().Begin()
	if rule.Name != oldName {
		if err := tx.Table(model.TableNameAuthItem).Where("rule_name = ?", oldName).Update("rule_name", rule.Name).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	rule.UpdatedAt = uint32(time.Now().Unix())
	err := tx.Table(model.TableNameAuthRule).Where("name = ?", oldName).Updates(map[string]any{
		"name":       rule.Name,
		"data":       rule.Data,
		"updated_at": rule.UpdatedAt,
	}).Error
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

func (m *RbacManager) RemoveRule(name string) error {
	tx := m.db().Begin()
	if err := tx.Table(model.TableNameAuthItem).Where("rule_name = ?", name).Update("rule_name", nil).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Table(model.TableNameAuthRule).Where("name = ?", name).Delete(nil).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

// ---------- children (hierarchy) ----------

// GetChildren returns direct children of an item — one hop, not
// transitive. For the full downward closure use GetChildRoles or
// GetPermissionsByRole which fold in getChildrenRecursive.
func (m *RbacManager) GetChildren(parent string) map[string]*model.AuthItem {
	out := map[string]*model.AuthItem{}
	rows := []*model.AuthItem{}
	err := m.db().Table(model.TableNameAuthItem+" AS i").
		Joins("JOIN "+model.TableNameAuthItemChild+" AS c ON c.child = i.name").
		Where("c.parent = ?", parent).
		Find(&rows).Error
	if err != nil {
		config.Log.Errorf("rbac: GetChildren(%s): %v", parent, err)
		return out
	}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func (m *RbacManager) HasChild(parent, child string) bool {
	var count int64
	m.db().Table(model.TableNameAuthItemChild).Where("parent = ? AND child = ?", parent, child).Count(&count)
	return count > 0
}

// CanAddChild wraps detectLoop — same shape as DbManager::canAddChild.
func (m *RbacManager) CanAddChild(parent, child *model.AuthItem) bool {
	if parent == nil || child == nil {
		return false
	}
	return !m.detectLoop(parent, child)
}

// AddChild inserts a parent→child edge, rejecting three shapes that
// would corrupt the graph: self-edge, permission→role (Yii2 rule
// against roles as children of permissions), and any edge that would
// close a cycle.
func (m *RbacManager) AddChild(parent, child *model.AuthItem) error {
	if parent == nil || child == nil {
		return errors.New("rbac: AddChild needs both nodes")
	}
	if parent.Name == child.Name {
		return fmt.Errorf("rbac: cannot add %q as a child of itself", parent.Name)
	}
	if parent.Type == RbacItemTypePermission && child.Type == RbacItemTypeRole {
		return errors.New("rbac: cannot add a role as a child of a permission")
	}
	if m.detectLoop(parent, child) {
		return fmt.Errorf("rbac: cannot add %q as child of %q; a loop would form", child.Name, parent.Name)
	}
	row := &model.AuthItemChild{Parent: parent.Name, Child: child.Name}
	if err := m.db().Table(model.TableNameAuthItemChild).Create(row).Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

func (m *RbacManager) RemoveChild(parent, child *model.AuthItem) error {
	if parent == nil || child == nil {
		return errors.New("rbac: RemoveChild needs both nodes")
	}
	if err := m.db().Table(model.TableNameAuthItemChild).
		Where("parent = ? AND child = ?", parent.Name, child.Name).
		Delete(nil).Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

func (m *RbacManager) RemoveChildren(parent *model.AuthItem) error {
	if parent == nil {
		return errors.New("rbac: RemoveChildren needs a parent")
	}
	if err := m.db().Table(model.TableNameAuthItemChild).Where("parent = ?", parent.Name).Delete(nil).Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

// detectLoop mirrors DbManager::detectLoop. Uses the cached child
// list to stay O(V+E) instead of hitting the DB per recursion — the
// PHP version does one query per call and can be slow on wide graphs.
func (m *RbacManager) detectLoop(parent, child *model.AuthItem) bool {
	if child.Name == parent.Name {
		return true
	}
	m.loadCache()
	m.cacheMu.RLock()
	// Build parent → []children from parents-of map for fast walk.
	// Doing this once per call is cheap; the parents map is small.
	childrenOf := map[string][]string{}
	for c, ps := range m.parents {
		for _, p := range ps {
			childrenOf[p] = append(childrenOf[p], c)
		}
	}
	m.cacheMu.RUnlock()

	stack := append([]string{}, childrenOf[child.Name]...)
	seen := map[string]bool{child.Name: true}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == parent.Name {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, childrenOf[n]...)
	}
	return false
}

// ---------- assignments ----------

// GetAssignment returns the (userId, roleName) row or nil. Never
// consults the item/parent cache — assignments are inherently
// per-user state.
func (m *RbacManager) GetAssignment(roleName string, userId uint32) *model.AuthAssignment {
	if userId == 0 {
		return nil
	}
	a, _ := activerecord.FindOneWhere[model.AuthAssignment]("`user_id` = ? AND `item_name` = ?", userId, roleName)
	return a
}

// GetAssignments returns every direct assignment for a user, keyed
// by item_name. Default roles are NOT included here (matching Yii2);
// checkAccessFromCache adds them at check time.
func (m *RbacManager) GetAssignments(userId uint32) map[string]*model.AuthAssignment {
	out := map[string]*model.AuthAssignment{}
	if userId == 0 {
		return out
	}
	rows := []*model.AuthAssignment{}
	if err := m.db().Table(model.TableNameAuthAssignment).Where("user_id = ?", userId).Find(&rows).Error; err != nil {
		config.Log.Errorf("rbac: GetAssignments(%d): %v", userId, err)
		return out
	}
	for _, r := range rows {
		out[r.ItemName] = r
	}
	return out
}

// Assign wires a role/permission to a user. Returns the created
// AuthAssignment. Idempotent-ish: the composite PK prevents dupes;
// callers should catch the duplicate-key error if they retry.
func (m *RbacManager) Assign(role *model.AuthItem, userId uint32) (*model.AuthAssignment, error) {
	if role == nil {
		return nil, errors.New("rbac: Assign needs a role")
	}
	if userId == 0 {
		return nil, errors.New("rbac: Assign needs a userId")
	}
	now := uint32(time.Now().Unix())
	a := &model.AuthAssignment{
		UserId:    userId,
		ItemName:  role.Name,
		CreatedAt: &now,
	}
	if err := m.db().Table(model.TableNameAuthAssignment).Create(a).Error; err != nil {
		return nil, err
	}
	m.forgetAssignments(userId)
	return a, nil
}

func (m *RbacManager) Revoke(role *model.AuthItem, userId uint32) error {
	if role == nil || userId == 0 {
		return errors.New("rbac: Revoke needs role and userId")
	}
	if err := m.db().Table(model.TableNameAuthAssignment).
		Where("user_id = ? AND item_name = ?", userId, role.Name).
		Delete(nil).Error; err != nil {
		return err
	}
	m.forgetAssignments(userId)
	return nil
}

func (m *RbacManager) RevokeAll(userId uint32) error {
	if userId == 0 {
		return errors.New("rbac: RevokeAll needs a userId")
	}
	if err := m.db().Table(model.TableNameAuthAssignment).Where("user_id = ?", userId).Delete(nil).Error; err != nil {
		return err
	}
	m.forgetAssignments(userId)
	return nil
}

func (m *RbacManager) forgetAssignments(userId uint32) {
	m.assignmentsMu.Lock()
	delete(m.assignmentsCache, userId)
	m.assignmentsMu.Unlock()
}

// ---------- role/permission lookups ----------

// GetRolesByUser returns direct roles plus default roles, same as
// DbManager::getRolesByUser.
func (m *RbacManager) GetRolesByUser(userId uint32) map[string]*model.AuthItem {
	out := map[string]*model.AuthItem{}
	for _, dr := range m.GetDefaultRoles() {
		if it := m.GetItem(dr); it != nil {
			out[dr] = it
		}
	}
	if userId == 0 {
		return out
	}
	rows := []*model.AuthItem{}
	err := m.db().Table(model.TableNameAuthItem+" AS b").
		Joins("JOIN "+model.TableNameAuthAssignment+" AS a ON a.item_name = b.name").
		Where("a.user_id = ? AND b.type = ?", userId, RbacItemTypeRole).
		Find(&rows).Error
	if err != nil {
		config.Log.Errorf("rbac: GetRolesByUser(%d): %v", userId, err)
		return out
	}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

// GetChildRoles returns the role plus every role reachable from it
// through the child hierarchy — only ROLE-typed nodes.
func (m *RbacManager) GetChildRoles(roleName string) map[string]*model.AuthItem {
	role := m.GetRole(roleName)
	if role == nil {
		return map[string]*model.AuthItem{}
	}
	m.loadCache()
	childrenList := m.getChildrenList()
	found := map[string]bool{}
	m.getChildrenRecursive(roleName, childrenList, found)

	out := map[string]*model.AuthItem{roleName: role}
	roles := m.GetRoles()
	for name := range found {
		if r, ok := roles[name]; ok {
			out[name] = r
		}
	}
	return out
}

// GetPermissionsByRole returns every permission-typed descendant of
// the role — direct or through intermediate roles.
func (m *RbacManager) GetPermissionsByRole(roleName string) map[string]*model.AuthItem {
	m.loadCache()
	childrenList := m.getChildrenList()
	found := map[string]bool{}
	m.getChildrenRecursive(roleName, childrenList, found)
	if len(found) == 0 {
		return map[string]*model.AuthItem{}
	}
	names := make([]string, 0, len(found))
	for k := range found {
		names = append(names, k)
	}
	rows := []*model.AuthItem{}
	if err := m.db().Table(model.TableNameAuthItem).Where("type = ? AND name IN ?", RbacItemTypePermission, names).Find(&rows).Error; err != nil {
		config.Log.Errorf("rbac: GetPermissionsByRole(%s): %v", roleName, err)
	}
	out := map[string]*model.AuthItem{}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

// GetPermissionsByUser = direct + inherited, matching DbManager.
func (m *RbacManager) GetPermissionsByUser(userId uint32) map[string]*model.AuthItem {
	direct := m.getDirectPermissionsByUser(userId)
	inherited := m.getInheritedPermissionsByUser(userId)
	for k, v := range inherited {
		direct[k] = v
	}
	return direct
}

func (m *RbacManager) getDirectPermissionsByUser(userId uint32) map[string]*model.AuthItem {
	out := map[string]*model.AuthItem{}
	if userId == 0 {
		return out
	}
	rows := []*model.AuthItem{}
	err := m.db().Table(model.TableNameAuthItem+" AS b").
		Joins("JOIN "+model.TableNameAuthAssignment+" AS a ON a.item_name = b.name").
		Where("a.user_id = ? AND b.type = ?", userId, RbacItemTypePermission).
		Find(&rows).Error
	if err != nil {
		config.Log.Errorf("rbac: getDirectPermissionsByUser(%d): %v", userId, err)
		return out
	}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func (m *RbacManager) getInheritedPermissionsByUser(userId uint32) map[string]*model.AuthItem {
	out := map[string]*model.AuthItem{}
	if userId == 0 {
		return out
	}
	var itemNames []string
	if err := m.db().Table(model.TableNameAuthAssignment).Where("user_id = ?", userId).Pluck("item_name", &itemNames).Error; err != nil {
		config.Log.Errorf("rbac: getInheritedPermissionsByUser(%d) pluck: %v", userId, err)
		return out
	}
	m.loadCache()
	childrenList := m.getChildrenList()
	found := map[string]bool{}
	for _, name := range itemNames {
		m.getChildrenRecursive(name, childrenList, found)
	}
	if len(found) == 0 {
		return out
	}
	names := make([]string, 0, len(found))
	for k := range found {
		names = append(names, k)
	}
	rows := []*model.AuthItem{}
	if err := m.db().Table(model.TableNameAuthItem).Where("type = ? AND name IN ?", RbacItemTypePermission, names).Find(&rows).Error; err != nil {
		config.Log.Errorf("rbac: getInheritedPermissionsByUser(%d) items: %v", userId, err)
		return out
	}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

// getChildrenList is the parent → []child inverse used by
// getChildrenRecursive. Built from the cached parents map.
func (m *RbacManager) getChildrenList() map[string][]string {
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	out := map[string][]string{}
	for c, ps := range m.parents {
		for _, p := range ps {
			out[p] = append(out[p], c)
		}
	}
	return out
}

func (m *RbacManager) getChildrenRecursive(name string, childrenList map[string][]string, result map[string]bool) {
	for _, child := range childrenList[name] {
		if result[child] {
			continue
		}
		result[child] = true
		m.getChildrenRecursive(child, childrenList, result)
	}
}

// GetUserIdsByRole returns every user id assigned the role (directly
// or via a descendant role). Matches DbManager::getUserIdsByRole
// which folds childrenRecursive over the role.
func (m *RbacManager) GetUserIdsByRole(roleName string) []uint32 {
	m.loadCache()
	childrenList := m.getChildrenList()
	found := map[string]bool{roleName: true}
	m.getChildrenRecursive(roleName, childrenList, found)

	names := make([]string, 0, len(found))
	for k := range found {
		names = append(names, k)
	}
	var ids []uint32
	if err := m.db().Table(model.TableNameAuthAssignment).Where("item_name IN ?", names).Distinct("user_id").Pluck("user_id", &ids).Error; err != nil {
		config.Log.Errorf("rbac: GetUserIdsByRole(%s): %v", roleName, err)
	}
	return ids
}

// ---------- bulk removals ----------

func (m *RbacManager) RemoveAll() error {
	tx := m.db().Begin()
	for _, tbl := range []string{model.TableNameAuthAssignment, model.TableNameAuthItemChild, model.TableNameAuthItem, model.TableNameAuthRule} {
		if err := tx.Exec("DELETE FROM `" + tbl + "`").Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

func (m *RbacManager) RemoveAllPermissions() error { return m.removeAllItems(RbacItemTypePermission) }
func (m *RbacManager) RemoveAllRoles() error       { return m.removeAllItems(RbacItemTypeRole) }
func (m *RbacManager) removeAllItems(itemType int) error {
	tx := m.db().Begin()
	var names []string
	if err := tx.Table(model.TableNameAuthItem).Where("type = ?", itemType).Pluck("name", &names).Error; err != nil {
		tx.Rollback()
		return err
	}
	if len(names) > 0 {
		if err := tx.Table(model.TableNameAuthItemChild).Where("parent IN ? OR child IN ?", names, names).Delete(nil).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Table(model.TableNameAuthAssignment).Where("item_name IN ?", names).Delete(nil).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Table(model.TableNameAuthItem).Where("type = ?", itemType).Delete(nil).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

func (m *RbacManager) RemoveAllRules() error {
	tx := m.db().Begin()
	if err := tx.Table(model.TableNameAuthItem).Where("rule_name IS NOT NULL").Update("rule_name", nil).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM `" + model.TableNameAuthRule + "`").Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

func (m *RbacManager) RemoveAllAssignments() error {
	if err := m.db().Exec("DELETE FROM `" + model.TableNameAuthAssignment + "`").Error; err != nil {
		return err
	}
	m.assignmentsMu.Lock()
	m.assignmentsCache = map[uint32]map[string]*model.AuthAssignment{}
	m.assignmentsMu.Unlock()
	return nil
}

// ---------- transport helpers ----------

// The gRPC handler needs a wire-friendly view of an item — the
// underlying model has *string / *uint32 pointers that JSON-serialise
// awkwardly. Transform() flattens them, and the `data` blob is
// surfaced as a decoded map when it parses as JSON so admin UIs can
// read arbitrary metadata without a second decode.
type TransformRbacItem struct {
	Name        string         `json:"name"`
	Type        int            `json:"type"`
	Description string         `json:"description"`
	RuleName    string         `json:"rule_name"`
	Data        map[string]any `json:"data,omitempty"`
	CreatedAt   uint32         `json:"created_at"`
	UpdatedAt   uint32         `json:"updated_at"`
}

func TransformRbacItemModel(i *model.AuthItem) *TransformRbacItem {
	if i == nil {
		return nil
	}
	out := &TransformRbacItem{
		Name:        i.Name,
		Type:        i.Type,
		Description: i.GetDescription(),
		RuleName:    i.GetRuleName(),
		CreatedAt:   i.CreatedAt,
		UpdatedAt:   i.UpdatedAt,
	}
	if s := i.GetData(); s != "" {
		var m map[string]any
		if json.Unmarshal([]byte(s), &m) == nil {
			out.Data = m
		}
	}
	return out
}

// ErrRbac wraps engine errors in an enum-friendly message so gRPC
// callers get a stable "no permission" surface when access is denied
// upstream — matches how the codebase raises enum.NoPermission today.
var ErrRbacDenied = errors.New(enum.NoPermission)
