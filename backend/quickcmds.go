package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// QuickCmd is a user-defined terminal quick command persisted to a JSON file
// whose path is resolved from an environment variable (HARNESS_QUICK_CMDS_FILE,
// defaulting into TRIM_PKGVAR). The frontend manages the full list and saves it
// as a whole on each add/edit/delete.
type QuickCmd struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Auto    bool   `json:"auto"`
}

// quickCmdsFile is the on-disk envelope. A version field keeps the format
// forward-compatible.
type quickCmdsFile struct {
	Version  int        `json:"version"`
	Commands []QuickCmd `json:"commands"`
}

// quickCmdsMu serializes concurrent reads/writes of the persisted file.
var quickCmdsMu sync.Mutex

// quickCmdsPath resolves the persistence file path from the environment.
func quickCmdsPath(renv *RuntimeEnv) string {
	if renv != nil && renv.QuickCmdsFile != "" {
		return renv.QuickCmdsFile
	}
	return envOr("HARNESS_QUICK_CMDS_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "quickcmds.json"))
}

// handleGetQuickCmds returns the saved quick commands (or an empty list when
// the file does not exist yet).
func (m *AdminMux) handleGetQuickCmds(w http.ResponseWriter, r *http.Request) {
	path := quickCmdsPath(m.renv)
	cmds, err := loadQuickCmds(path)
	if err != nil {
		writeErr(w, "failed to load quick commands: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "commands": cmds})
}

// handleSaveQuickCmds validates and persists the whole quick command list.
func (m *AdminMux) handleSaveQuickCmds(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Commands []QuickCmd `json:"commands"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, "invalid request body", http.StatusBadRequest)
		return
	}
	cmds := make([]QuickCmd, 0, len(req.Commands))
	seen := map[string]bool{}
	for _, c := range req.Commands {
		c.Name = strings.TrimSpace(c.Name)
		c.Content = strings.TrimSpace(c.Content)
		if c.ID == "" || c.Name == "" || c.Content == "" {
			writeErr(w, "quick command requires id, name and content", http.StatusBadRequest)
			return
		}
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		cmds = append(cmds, c)
	}
	path := quickCmdsPath(m.renv)
	if err := saveQuickCmds(path, cmds); err != nil {
		writeErr(w, "failed to save quick commands: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "commands": cmds})
}

// loadQuickCmds reads the persisted file. A missing file yields an empty list.
// It also tolerates a legacy bare-array format.
func loadQuickCmds(path string) ([]QuickCmd, error) {
	quickCmdsMu.Lock()
	defer quickCmdsMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []QuickCmd{}, nil
		}
		return nil, err
	}
	var f quickCmdsFile
	if err := json.Unmarshal(data, &f); err != nil {
		var bare []QuickCmd
		if err2 := json.Unmarshal(data, &bare); err2 == nil {
			if bare == nil {
				return []QuickCmd{}, nil
			}
			return bare, nil
		}
		return nil, err
	}
	if f.Commands == nil {
		return []QuickCmd{}, nil
	}
	return f.Commands, nil
}

// saveQuickCmds atomically writes the command list (tmp file + rename).
func saveQuickCmds(path string, cmds []QuickCmd) error {
	quickCmdsMu.Lock()
	defer quickCmdsMu.Unlock()
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(quickCmdsFile{Version: 1, Commands: cmds}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
