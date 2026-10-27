package rbac

// rbac.services.go — the RBAC transport layer. Callers pass ?key=<op>
// and the coreService CRUD switch defers to Dispatch below; every op
// maps 1:1 to a yii\rbac\DbManager method. The engine itself lives
// in pkg/core/rbac/rbac.manager.go.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"app/pkg/core/rbac/model"
	"app/pkg/core/reqctx"
	"app/pkg/enum"
	"app/pkg/protos/gen"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/lib"
	"github.com/gin-gonic/gin"
)

type RbacService interface {
	Dispatch() (*gen.Response, bool, error)
}

// rbacService bundles the request context. We DON'T embed
// register.GrpcAction — pkg/core/register imports pkg/core/middleware,
// and the RBAC middleware imports this package, so an embed would
// close an import cycle. The two helpers we'd have inherited
// (IsAdmin / Response) are trivial and are re-implemented as
// methods below.
type rbacService struct {
	c *action.Context
}

func NewRbacService(in *gen.Request, params ...map[string]any) RbacService {
	if in == nil {
		in = &gen.Request{}
	}
	c := action.Request(in.Query)
	for _, p := range params {
		c.SetQueryParams(p)
	}
	return &rbacService{c: c}
}

// IsAdmin mirrors register.GrpcAction.IsAdmin — kept local to avoid
// importing register (see the type-doc for why).
func (srv *rbacService) IsAdmin() bool { return reqctx.UserID() == enum.ADMIN_ID }

// Response builds the gen.Response envelope the rest of the codebase
// uses. Mirrors register.GrpcAction.Response so callers get the same
// shape they'd get from any coreService method.
func (srv *rbacService) Response(data any) (*gen.Response, error) {
	res := &gen.Response{Status: http.StatusOK, Message: "success"}
	if !lib.IsEmpty(data) {
		if b := lib.ToMarshal(data); !lib.IsEmpty(b) {
			res.Data = b
		}
	}
	return res, nil
}

// rbacDispatch inspects ?key and, when it matches a known RBAC
// operation, runs it against the manager and returns (response, true,
// err). When key doesn't match, returns (nil, false, nil) so the
// original CRUD switch keeps handling the request.
func (srv *rbacService) Dispatch() (*gen.Response, bool, error) {
	// Use srv.c.Query (populated from the passed `in`) rather than
	// GrpcAction.Query — the latter reads goroutine-local storage and
	// silently disagrees when a caller hands us a different request.
	key := srv.c.Query("key")
	if key == "" {
		return nil, false, nil
	}
	switch key {
	// ---------- reads ----------
	case "check-access":
		userId := lib.ToUint32(srv.c.Query("user_id"))
		perm := srv.c.Query("permission")
		if userId == 0 {
			userId = reqctx.UserID()
		}
		ok := Rbac().CheckAccess(userId, perm, srv.rbacParams())
		rs, err := srv.Response(gin.H{"allowed": ok, "user_id": userId, "permission": perm})
		return rs, true, err

	case "roles-by-user":
		userId := srv.rbacUserId()
		items := Rbac().GetRolesByUser(userId)
		rs, err := srv.Response(transformRbacItems(items))
		return rs, true, err

	case "permissions-by-user":
		userId := srv.rbacUserId()
		items := Rbac().GetPermissionsByUser(userId)
		rs, err := srv.Response(transformRbacItems(items))
		return rs, true, err

	case "permissions-by-role":
		items := Rbac().GetPermissionsByRole(srv.c.Query("role"))
		rs, err := srv.Response(transformRbacItems(items))
		return rs, true, err

	case "child-roles":
		items := Rbac().GetChildRoles(srv.c.Query("role"))
		rs, err := srv.Response(transformRbacItems(items))
		return rs, true, err

	case "assignments":
		userId := srv.rbacUserId()
		rs, err := srv.Response(Rbac().GetAssignments(userId))
		return rs, true, err

	case "children":
		items := Rbac().GetChildren(srv.c.Query("name"))
		rs, err := srv.Response(transformRbacItems(items))
		return rs, true, err

	case "has-child":
		ok := Rbac().HasChild(srv.c.Query("parent"), srv.c.Query("child"))
		rs, err := srv.Response(gin.H{"has_child": ok})
		return rs, true, err

	case "roles":
		rs, err := srv.Response(transformRbacItems(Rbac().GetRoles()))
		return rs, true, err

	case "permissions":
		rs, err := srv.Response(transformRbacItems(Rbac().GetPermissions()))
		return rs, true, err

	case "rules":
		rs, err := srv.Response(Rbac().GetRules())
		return rs, true, err

	case "users-by-role":
		role := srv.c.Query("role")
		ids := Rbac().GetUserIdsByRole(role)
		rs, err := srv.Response(gin.H{"role": role, "user_ids": ids})
		return rs, true, err

	// ---------- writes ----------
	case "assign":
		if !srv.IsAdmin() {
			return nil, true, errors.New(enum.NoPermission)
		}
		role := Rbac().GetItem(srv.c.Query("role"))
		if role == nil {
			return nil, true, errors.New("rbac: role not found")
		}
		userId := srv.rbacUserId()
		a, err := Rbac().Assign(role, userId)
		if err != nil {
			return nil, true, err
		}
		rs, err := srv.Response(a)
		return rs, true, err

	case "revoke":
		if !srv.IsAdmin() {
			return nil, true, errors.New(enum.NoPermission)
		}
		role := Rbac().GetItem(srv.c.Query("role"))
		if role == nil {
			return nil, true, errors.New("rbac: role not found")
		}
		userId := srv.rbacUserId()
		if err := Rbac().Revoke(role, userId); err != nil {
			return nil, true, err
		}
		rs, err := srv.Response(gin.H{"revoked": true, "role": role.Name, "user_id": userId})
		return rs, true, err

	case "revoke-all":
		if !srv.IsAdmin() {
			return nil, true, errors.New(enum.NoPermission)
		}
		userId := srv.rbacUserId()
		if err := Rbac().RevokeAll(userId); err != nil {
			return nil, true, err
		}
		rs, err := srv.Response(gin.H{"revoked_all": true, "user_id": userId})
		return rs, true, err

	case "add-child":
		if !srv.IsAdmin() {
			return nil, true, errors.New(enum.NoPermission)
		}
		parent := Rbac().GetItem(srv.c.Query("parent"))
		child := Rbac().GetItem(srv.c.Query("child"))
		if parent == nil || child == nil {
			return nil, true, errors.New("rbac: parent or child not found")
		}
		if err := Rbac().AddChild(parent, child); err != nil {
			return nil, true, err
		}
		rs, err := srv.Response(gin.H{"parent": parent.Name, "child": child.Name})
		return rs, true, err

	case "remove-child":
		if !srv.IsAdmin() {
			return nil, true, errors.New(enum.NoPermission)
		}
		parent := Rbac().GetItem(srv.c.Query("parent"))
		child := Rbac().GetItem(srv.c.Query("child"))
		if parent == nil || child == nil {
			return nil, true, errors.New("rbac: parent or child not found")
		}
		if err := Rbac().RemoveChild(parent, child); err != nil {
			return nil, true, err
		}
		rs, err := srv.Response(gin.H{"removed": true, "parent": parent.Name, "child": child.Name})
		return rs, true, err

	case "remove-children":
		if !srv.IsAdmin() {
			return nil, true, errors.New(enum.NoPermission)
		}
		parent := Rbac().GetItem(srv.c.Query("parent"))
		if parent == nil {
			return nil, true, errors.New("rbac: parent not found")
		}
		if err := Rbac().RemoveChildren(parent); err != nil {
			return nil, true, err
		}
		rs, err := srv.Response(gin.H{"removed_children_of": parent.Name})
		return rs, true, err

	case "invalidate-cache":
		Rbac().InvalidateCache()
		rs, err := srv.Response(gin.H{"invalidated": true})
		return rs, true, err

	case "set-default-roles":
		if !srv.IsAdmin() {
			return nil, true, errors.New(enum.NoPermission)
		}
		names := splitAndTrim(srv.c.Query("roles"), ",")
		Rbac().SetDefaultRoles(names...)
		rs, err := srv.Response(gin.H{"default_roles": Rbac().GetDefaultRoles()})
		return rs, true, err

	case "default-roles":
		rs, err := srv.Response(gin.H{"default_roles": Rbac().GetDefaultRoles()})
		return rs, true, err
	}
	return nil, false, nil
}

// rbacUserId reads user_id from the query and falls back to the
// caller's own id when omitted. Matches the "who am I asking about"
// convention used elsewhere in this file.
func (srv *rbacService) rbacUserId() uint32 {
	id := lib.ToUint32(srv.c.Query("user_id"))
	if id == 0 {
		id = reqctx.UserID()
	}
	return id
}

// rbacParams collects any params_* query keys into the map that
// gets threaded through executeRule. Skipping this for now keeps
// the RPC lean; rules that need context get it from the userId
// or by re-loading from the DB.
func (srv *rbacService) rbacParams() map[string]any {
	out := map[string]any{}
	if raw := srv.c.Query("params"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			out["params"] = n
		} else {
			out["params"] = raw
		}
	}
	return out
}

// splitAndTrim splits on sep and drops empty segments after trimming
// whitespace, so ?roles=Admin,%20User yields two clean names.
func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// transformRbacItems flattens the map[name]*AuthItem shape the
// engine returns into a plain slice with the transport-friendly
// TransformRbacItem view. Preserves iteration order-instability
// but the map key is also inside each element for callers that need
// deterministic sorting.
func transformRbacItems(m map[string]*model.AuthItem) []*TransformRbacItem {
	out := make([]*TransformRbacItem, 0, len(m))
	for _, it := range m {
		out = append(out, TransformRbacItemModel(it))
	}
	return out
}
