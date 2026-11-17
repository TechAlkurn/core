package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BackupOptions controls what mysqldump captures and where it writes.
type BackupOptions struct {
	// Dir is the target directory for the .sql file. Created if missing.
	// If empty, defaults to "<DIR_STORAGE>/db_backups", or "./backups" if
	// DIR_STORAGE isn't set.
	Dir string
	// SchemaOnly adds --no-data — dumps CREATE TABLE / routines only.
	SchemaOnly bool
	// FileName overrides the default "<dbname>-<kind>-<timestamp>.sql".
	// Callers wanting a stable path (e.g. for links in a response) can pin
	// the name themselves. If unset, the timestamped default is used.
	FileName string
}

// DumpDatabase runs mysqldump against the app's configured database using
// DB_USER / DB_PASS / DB_NAME / DB_HOST / DB_PORT env vars. Password is
// passed via the MYSQL_PWD environment variable — never on the argv — so
// it doesn't leak through ps(1) or process listings.
//
// The dump uses --single-transaction (InnoDB-consistent snapshot without
// locking writes), plus --routines / --triggers / --events so a full
// restore reproduces the app's stored logic, not just the tables.
//
// Returns the absolute path of the written file on success. On any error,
// the (potentially truncated) output file is removed so callers can't
// mistake a partial dump for a good one.

// envOr returns the trimmed env value for key, or def if unset/empty.
// Never calls log.Fatal — a missing optional env var must not tear down
// the whole gRPC process just because a backup was requested.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func DumpDatabase(opts BackupOptions) (string, error) {
	// Only user + database name are strictly required — everything else
	// has a sensible default. Password can legitimately be empty for
	// local dev on a socket connection.
	dbUser := envOr("DB_USER", "")
	dbName := envOr("DB_NAME", "")
	if dbUser == "" || dbName == "" {
		return "", errors.New("DB_USER and DB_NAME must be set to run mysqldump")
	}
	dbPass := envOr("DB_PASS", "")
	dbHost := envOr("DB_HOST", "localhost")
	dbPort := envOr("DB_PORT", "3306")

	dir := opts.Dir
	if dir == "" {
		if storage := os.Getenv("DIR_DB"); storage != "" {
			dir = filepath.Join(storage, "backups")
		} else {
			dir = "backups"
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir %q: %w", dir, err)
	}

	name := opts.FileName
	if name == "" {
		kind := "full"
		if opts.SchemaOnly {
			kind = "schema"
		}
		name = fmt.Sprintf("%s-%s-%s.sql", dbName, kind, time.Now().Format("20060102-150405"))
	}
	outPath := filepath.Join(dir, name)

	// Confirm the binary exists before we open the output file — otherwise
	// we'd leave a zero-byte .sql behind on every unconfigured deploy.
	if _, err := exec.LookPath("mysqldump"); err != nil {
		return "", fmt.Errorf("mysqldump not available in PATH: %w", err)
	}

	args := []string{
		"-u", dbUser,
		"-h", dbHost,
		"-P", dbPort,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--events",
		"--skip-lock-tables",
	}
	if opts.SchemaOnly {
		args = append(args, "--no-data")
	}
	args = append(args, dbName)
	out, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("open backup file %q: %w", outPath, err)
	}
	// Belt-and-braces: if mysqldump errors, remove the partial file so
	// nothing on disk looks like a valid snapshot. Deferred so a panic
	// mid-run doesn't leave garbage either.
	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(outPath)
		}
	}()

	stderr := &bytes.Buffer{}
	cmd := exec.Command("mysqldump", args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+dbPass)
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mysqldump: %w — %s", err, bytes.TrimSpace(stderr.Bytes()))
	}

	success = true
	abs, err := filepath.Abs(outPath)
	if err != nil {
		return outPath, nil
	}
	return abs, nil
}

// RestoreOptions controls a database restore from an existing dump file.
type RestoreOptions struct {
	// File is the .sql dump to import. Absolute or relative to the app's
	// working directory. Required.
	File string
	// TargetDB overrides DB_NAME so a dump can be restored into a scratch
	// database (e.g. for verifying a snapshot before replacing prod).
	// Empty uses the current DB_NAME.
	TargetDB string
	// BackupBefore takes a full DumpDatabase snapshot right before the
	// restore starts. If that snapshot fails, the restore does not run —
	// same fail-closed safety contract as ResetDBTables.
	BackupBefore bool
	// AllowedDir constrains File to live under this directory. Web
	// endpoints should always set this to the backups directory so a
	// caller can't feed in an arbitrary .sql from elsewhere on disk.
	// Empty disables the check (fine for CLI callers).
	AllowedDir string
}

// RestoreDatabase pipes the SQL file at opts.File through the `mysql`
// client into the configured database. Uses the same env-based
// credentials as DumpDatabase and the same MYSQL_PWD-via-env trick to
// keep the password off the process argv.
//
// The mysqldump output produced by DumpDatabase already includes SET
// @OLD_FOREIGN_KEY_CHECKS / SET FOREIGN_KEY_CHECKS = 0 pragmas at the
// top and restores them at the end, so restore doesn't need to wrap the
// call in its own FK-checks toggling — the SQL takes care of itself.
//
// Returns the absolute path of the file that was actually restored
// (mostly useful for logs / responses).
func RestoreDatabase(opts RestoreOptions) (string, error) {
	if strings.TrimSpace(opts.File) == "" {
		return "", errors.New("File is required")
	}
	absFile, err := filepath.Abs(opts.File)
	if err != nil {
		return "", fmt.Errorf("resolve restore file path: %w", err)
	}
	// Path containment check. Rel returns "..<something>" when the file
	// escapes AllowedDir; anything starting with ".." means outside.

	allowedDir := opts.AllowedDir
	if allowedDir == "" {
		if storage := os.Getenv("DIR_DB"); storage != "" {
			allowedDir = filepath.Join(storage, "backups")
		} else {
			allowedDir = "backups"
		}
	}

	if allowedDir != "" {
		allowed, err := filepath.Abs(allowedDir)
		if err != nil {
			return "", fmt.Errorf("resolve allowed dir: %w", err)
		}
		rel, err := filepath.Rel(allowed, absFile)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("restore file %q is outside allowed dir %q", absFile, allowed)
		}
	}
	info, err := os.Stat(absFile)
	if err != nil {
		return "", fmt.Errorf("stat restore file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("restore file %q is a directory", absFile)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("restore file %q is empty", absFile)
	}

	dbUser := envOr("DB_USER", "")
	dbName := envOr("DB_NAME", "")
	if dbUser == "" || dbName == "" {
		return "", errors.New("DB_USER and DB_NAME must be set to run mysql")
	}
	if opts.TargetDB != "" {
		dbName = opts.TargetDB
	}
	dbPass := envOr("DB_PASS", "")
	dbHost := envOr("DB_HOST", "localhost")
	dbPort := envOr("DB_PORT", "3306")

	if _, err := exec.LookPath("mysql"); err != nil {
		return "", fmt.Errorf("mysql client not available in PATH: %w", err)
	}

	// Optional pre-restore snapshot. Fails-closed: if the snapshot
	// errors, the restore never touches the target DB.
	var priorBackup string
	if opts.BackupBefore {
		priorBackup, err = DumpDatabase(BackupOptions{
			FileName: fmt.Sprintf("%s-pre-restore-%s.sql", dbName, time.Now().Format("20060102-150405")),
		})
		if err != nil {
			return "", fmt.Errorf("pre-restore backup failed: %w", err)
		}
	}

	in, err := os.Open(absFile)
	if err != nil {
		return "", fmt.Errorf("open restore file: %w", err)
	}
	defer in.Close()

	// --local-infile=0 disables client-side LOAD DATA LOCAL INFILE so an
	// attacker-supplied .sql can't turn a restore into an arbitrary-file
	// read against whatever the mysql client can see on disk.
	args := []string{
		"-u", dbUser,
		"-h", dbHost,
		"-P", dbPort,
		"--local-infile=0",
		dbName,
	}
	stderr := &bytes.Buffer{}
	cmd := exec.Command("mysql", args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+dbPass)
	cmd.Stdin = in
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if priorBackup != "" {
			return "", fmt.Errorf("mysql restore: %w — %s (pre-restore snapshot preserved at %s)",
				err, bytes.TrimSpace(stderr.Bytes()), priorBackup)
		}
		return "", fmt.Errorf("mysql restore: %w — %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return absFile, nil
}
