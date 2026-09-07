package ticket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTicketReadSuccess(t *testing.T) {
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "workspace")
	ticketDir := filepath.Join(workspaceRoot, ".architect", "tickets")
	if err := os.MkdirAll(ticketDir, 0755); err != nil {
		t.Fatal(err)
	}

	convID := "conv-12345"
	ticketContent := "# Active Session Plan\n\n- Step 1: Tests\n- Step 2: Implementation\n"
	ticketFile := filepath.Join(ticketDir, convID+".md")
	if err := os.WriteFile(ticketFile, []byte(ticketContent), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		workspaceRoot: workspaceRoot,
		homeDir:       tempDir,
	}

	mux := http.NewServeMux()
	mgr.Register(mux)

	// 1. Successful read of specific conversation ticket
	req := httptest.NewRequest(http.MethodGet, APIPath+"?conversationId="+convID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if !resp.OK {
		t.Fatalf("expected ok: true, got ok: false with error %q", resp.Error)
	}
	if resp.Path != ticketFile {
		t.Errorf("expected path %q, got %q", ticketFile, resp.Path)
	}
	if resp.Content != ticketContent {
		t.Errorf("expected content %q, got %q", ticketContent, resp.Content)
	}
}

func TestTicketFallback(t *testing.T) {
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "workspace")
	archDir := filepath.Join(workspaceRoot, ".architect")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}

	fallbackContent := "# Fallback Ticket\n\nDefault workspace plan.\n"
	fallbackFile := filepath.Join(archDir, "ticket.md")
	if err := os.WriteFile(fallbackFile, []byte(fallbackContent), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		workspaceRoot: workspaceRoot,
		homeDir:       tempDir,
	}

	mux := http.NewServeMux()
	mgr.Register(mux)

	// Conversation ticket does not exist, should fall back to ticket.md
	req := httptest.NewRequest(http.MethodGet, APIPath+"?conversationId=unknown-session", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if !resp.OK {
		t.Fatalf("expected ok: true, got ok: false with error %q", resp.Error)
	}
	if resp.Path != fallbackFile {
		t.Errorf("expected path %q, got %q", fallbackFile, resp.Path)
	}
	if resp.Content != fallbackContent {
		t.Errorf("expected content %q, got %q", fallbackContent, resp.Content)
	}
}

func TestTicketNotFound(t *testing.T) {
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		workspaceRoot: workspaceRoot,
		homeDir:       tempDir,
	}

	mux := http.NewServeMux()
	mgr.Register(mux)

	// Neither specific ticket nor ticket.md exists
	req := httptest.NewRequest(http.MethodGet, APIPath+"?conversationId=no-ticket", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error != "ticket not found" {
		t.Errorf("expected error %q, got %q", "ticket not found", resp.Error)
	}
}

func TestTicketMissingConversationId(t *testing.T) {
	mgr := New(Options{WorkspaceRoot: "/tmp"})
	mux := http.NewServeMux()
	mgr.Register(mux)

	req := httptest.NewRequest(http.MethodGet, APIPath, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Errorf("expected ok: false")
	}
	if resp.Error != "missing conversationId query parameter" {
		t.Errorf("expected error %q, got %q", "missing conversationId query parameter", resp.Error)
	}
}

func TestTicketMethodNotAllowed(t *testing.T) {
	mgr := New(Options{WorkspaceRoot: "/tmp"})
	mux := http.NewServeMux()
	mgr.Register(mux)

	req := httptest.NewRequest(http.MethodPost, APIPath+"?conversationId=test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestTicketPathTraversalProtection(t *testing.T) {
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		workspaceRoot: workspaceRoot,
		homeDir:       tempDir,
	}

	mux := http.NewServeMux()
	mgr.Register(mux)

	// 1. Traversal in conversationId
	req1 := httptest.NewRequest(http.MethodGet, APIPath+"?conversationId=../../etc/passwd", nil)
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for conversationId traversal, got %d", rec1.Code)
	}

	// 2. Traversal in workspaceRoot
	req2 := httptest.NewRequest(http.MethodGet, APIPath+"?conversationId=test&workspaceRoot=/etc", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for workspaceRoot traversal, got %d", rec2.Code)
	}
}

func TestTicketCustomWorkspaceRoot(t *testing.T) {
	tempDir := t.TempDir()
	customWs := filepath.Join(tempDir, "custom-workspace")
	ticketDir := filepath.Join(customWs, ".architect", "tickets")
	if err := os.MkdirAll(ticketDir, 0755); err != nil {
		t.Fatal(err)
	}

	ticketContent := "# Custom WS Ticket\n"
	ticketPath := filepath.Join(ticketDir, "session-abc.md")
	if err := os.WriteFile(ticketPath, []byte(ticketContent), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		workspaceRoot: filepath.Join(tempDir, "other-ws"),
		homeDir:       tempDir, // customWs is within tempDir
	}

	mux := http.NewServeMux()
	mgr.Register(mux)

	req := httptest.NewRequest(http.MethodGet, APIPath+"?conversationId=session-abc&workspaceRoot="+customWs, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Content != ticketContent {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
