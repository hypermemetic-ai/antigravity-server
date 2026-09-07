package ticket

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// APIPath is the endpoint for reading session tickets.
	APIPath = "/__agy/api/ticket"
)

// Options configures the ticket manager.
type Options struct {
	WorkspaceRoot string
}

// Manager handles reading active session tickets.
type Manager struct {
	workspaceRoot string
	homeDir       string
}

// New creates a new ticket Manager.
func New(opts Options) *Manager {
	home, _ := os.UserHomeDir()
	return &Manager{
		workspaceRoot: opts.WorkspaceRoot,
		homeDir:       home,
	}
}

// Register mounts the ticket endpoint on mux.
func (m *Manager) Register(mux *http.ServeMux) {
	mux.HandleFunc(APIPath, m.handleTicket)
}

// Response defines the JSON structure returned by the ticket API.
type Response struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

var (
	errForbidden = errors.New("access denied")
)

func (m *Manager) handleTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, Response{
			OK:    false,
			Error: "method not allowed",
		})
		return
	}

	convID := strings.TrimSpace(r.URL.Query().Get("conversationId"))
	if convID == "" {
		writeJSON(w, http.StatusBadRequest, Response{
			OK:    false,
			Error: "missing conversationId query parameter",
		})
		return
	}

	// Reject conversationId with path traversal or path separators
	if strings.Contains(convID, "/") || strings.Contains(convID, "\\") || strings.Contains(convID, "..") {
		writeJSON(w, http.StatusForbidden, Response{
			OK:    false,
			Error: "invalid conversationId",
		})
		return
	}

	wsRoot := strings.TrimSpace(r.URL.Query().Get("workspaceRoot"))
	if wsRoot == "" {
		wsRoot = m.workspaceRoot
	}
	if wsRoot == "" {
		wsRoot = "."
	}

	validatedRoot, err := m.validatePath(wsRoot)
	if err != nil {
		writeJSON(w, http.StatusForbidden, Response{
			OK:    false,
			Error: fmt.Sprintf("invalid workspaceRoot: %v", err),
		})
		return
	}

	resolvedPath, content, err := m.resolveAndReadTicket(validatedRoot, convID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, Response{
				OK:    false,
				Error: "ticket not found",
			})
			return
		}
		if errors.Is(err, errForbidden) {
			writeJSON(w, http.StatusForbidden, Response{
				OK:    false,
				Error: err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, Response{
			OK:    false,
			Error: fmt.Sprintf("failed to read ticket: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, Response{
		OK:      true,
		Path:    resolvedPath,
		Content: content,
	})
}

// resolveAndReadTicket looks for <workspaceRoot>/.architect/tickets/<conversationId>.md first.
// If not found, it falls back to scanning candidate project roots and then <workspaceRoot>/.architect/ticket.md.
func (m *Manager) resolveAndReadTicket(wsRoot string, convID string) (string, string, error) {
	candidateRoots := []string{wsRoot}
	if m.workspaceRoot != "" && m.workspaceRoot != wsRoot {
		candidateRoots = append(candidateRoots, m.workspaceRoot)
	}

	// 1. Try <root>/.architect/tickets/<convID>.md
	for _, root := range candidateRoots {
		ticketPath := filepath.Join(root, ".architect", "tickets", convID+".md")
		if validated, err := m.validatePath(ticketPath); err == nil {
			if content, err := os.ReadFile(validated); err == nil {
				return validated, string(content), nil
			} else if !os.IsNotExist(err) {
				return "", "", err
			}
		}
	}

	// 2. Fallback to <root>/.architect/ticket.md
	for _, root := range candidateRoots {
		fallbackPath := filepath.Join(root, ".architect", "ticket.md")
		if validated, err := m.validatePath(fallbackPath); err == nil {
			if content, err := os.ReadFile(validated); err == nil {
				return validated, string(content), nil
			} else if !os.IsNotExist(err) {
				return "", "", err
			}
		}
	}

	return "", "", os.ErrNotExist
}

// validatePath ensures that the path is within the allowed workspaceRoot or user home/gemini directory.
func (m *Manager) validatePath(inputPath string) (string, error) {
	if inputPath == "" {
		return "", errors.New("path cannot be empty")
	}

	if strings.HasPrefix(inputPath, "~/") || inputPath == "~" {
		if m.homeDir == "" {
			return "", errors.New("user home directory not found")
		}
		inputPath = filepath.Join(m.homeDir, strings.TrimPrefix(inputPath, "~"))
	}

	absPath, err := filepath.Abs(inputPath)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(absPath)

	var allowed []string
	if m.homeDir != "" {
		if absHome, err := filepath.Abs(m.homeDir); err == nil {
			allowed = append(allowed, filepath.Join(absHome, ".gemini"), absHome)
		} else {
			allowed = append(allowed, filepath.Join(m.homeDir, ".gemini"), m.homeDir)
		}
	}
	if m.workspaceRoot != "" {
		if absWs, err := filepath.Abs(m.workspaceRoot); err == nil {
			allowed = append(allowed, absWs)
		} else {
			allowed = append(allowed, m.workspaceRoot)
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		if absCwd, err := filepath.Abs(cwd); err == nil {
			allowed = append(allowed, absCwd)
		} else {
			allowed = append(allowed, cwd)
		}
	}

	isAllowed := false
	for _, root := range allowed {
		cleanRoot := filepath.Clean(root)
		if cleaned == cleanRoot || strings.HasPrefix(cleaned, cleanRoot+string(filepath.Separator)) {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		return "", fmt.Errorf("path %s is outside allowed directories (%s)", inputPath, strings.Join(allowed, ", "))
	}

	return cleaned, nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
