package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

type systemLogHub struct {
	mu        sync.Mutex
	lines     []string
	clients   map[*websocket.Conn]struct{}
	publicURL string
	upgrader  websocket.Upgrader
}

func newSystemLogHub(publicURL string) *systemLogHub {
	h := &systemLogHub{clients: make(map[*websocket.Conn]struct{}), publicURL: publicURL, lines: make([]string, 0, 5000)}
	h.upgrader = websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 4096, CheckOrigin: h.originAllowed}
	return h
}

func (h *systemLogHub) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}
	p, err := url.Parse(h.publicURL)
	return err == nil && strings.EqualFold(host, p.Hostname())
}

func (h *systemLogHub) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text == "" {
		return len(p), nil
	}
	parts := strings.Split(text, "\n")
	h.mu.Lock()
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		h.lines = append(h.lines, part)
		if len(h.lines) > 5000 {
			h.lines = append([]string(nil), h.lines[len(h.lines)-5000:]...)
		}
		for conn := range h.clients {
			if err := conn.WriteJSON(map[string]any{"type": "log", "line": part}); err != nil {
				_ = conn.Close()
				delete(h.clients, conn)
			}
		}
	}
	h.mu.Unlock()
	return len(p), nil
}

func (h *systemLogHub) snapshot(limit int) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit <= 0 || limit > len(h.lines) {
		limit = len(h.lines)
	}
	out := append([]string(nil), h.lines[len(h.lines)-limit:]...)
	return out
}

func (a *appServer) requireDeveloper(w http.ResponseWriter, r *http.Request) (memberSessionEntry, bool) {
	m, ok := a.currentMember(r)
	if !ok || m.Role != "developer" {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "developer access required"})
		return m, false
	}
	return m, true
}

func (a *appServer) developerDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actor, ok := a.requireDeveloper(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost && r.URL.Query().Get("action") == "clean" {
		var request struct {
			Challenge string `json:"challenge"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Challenge) != "CLEAN-DB-NOW" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "database clean challenge is invalid"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		tx, err := a.db.Pool().Begin(ctx)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer tx.Rollback(ctx)
		// Clean operational data while preserving schema, divisions/roles, and every developer account.
		statements := []string{
			`DELETE FROM support_message_reactions`, `DELETE FROM support_messages`, `DELETE FROM support_threads`,
			`DELETE FROM message_reactions`, `DELETE FROM messages`, `DELETE FROM conversation_members`, `DELETE FROM conversations`,
			`DELETE FROM comment_reactions`, `DELETE FROM post_reactions`, `DELETE FROM post_comments`, `DELETE FROM posts`,
			`DELETE FROM assessment_attempt_items`, `DELETE FROM assessment_attempts`, `DELETE FROM applications`,
			// Keep assessment_questions/assessment_choices: these are reusable division reference data, not operational attempts.
			`DELETE FROM announcements`,
			`DELETE FROM notifications`, `DELETE FROM bug_reports`, `DELETE FROM account_security_codes`, `DELETE FROM member_change_requests`,
			`DELETE FROM analytics_events`, `DELETE FROM member_presence_sessions`, `DELETE FROM archive_items`,
			`DELETE FROM media_library_usage`, `DELETE FROM media_library`,
			`DELETE FROM administration_member_positions`, `DELETE FROM administration_members`, `DELETE FROM audit_logs`,
			`DELETE FROM member_tags WHERE account_id NOT IN (SELECT ar.account_id FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE r.code='developer' AND ar.is_active=TRUE)`,
			`DELETE FROM account_roles WHERE account_id NOT IN (SELECT ar2.account_id FROM account_roles ar2 JOIN roles r2 ON r2.id=ar2.role_id WHERE r2.code='developer' AND ar2.is_active=TRUE)`,
			`DELETE FROM member_profiles WHERE account_id NOT IN (SELECT ar.account_id FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE r.code='developer' AND ar.is_active=TRUE)`,
			`DELETE FROM oauth_identities WHERE account_id NOT IN (SELECT ar.account_id FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE r.code='developer' AND ar.is_active=TRUE)`,
			`DELETE FROM accounts WHERE account_kind <> 'system' AND id NOT IN (SELECT ar.account_id FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE r.code='developer' AND ar.is_active=TRUE)`,
		}

		for _, q := range statements {
			if _, err = tx.Exec(ctx, q); err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "database clean stopped before completion"})
				return
			}
		}
		_, _ = tx.Exec(ctx, `INSERT INTO audit_logs(actor_account_id,action,entity_type,details) VALUES($1,'database.clean','database','{"scope":"operational"}'::jsonb)`, actor.AccountID)
		if tx.Commit(ctx) != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "message": "Operational records were cleared. Developer accounts, current divisions, roles, schema, and the assessment question bank were preserved."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	table := strings.TrimSpace(r.URL.Query().Get("table"))
	if table == "" {
		rows, err := a.db.Pool().Query(ctx, `SELECT t.tablename,COALESCE(s.n_live_tup,0)::bigint FROM pg_tables t LEFT JOIN pg_stat_user_tables s ON s.relname=t.tablename AND s.schemaname=t.schemaname WHERE t.schemaname='public' ORDER BY t.tablename`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "database overview unavailable"})
			return
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			var name string
			var estimate int64
			if rows.Scan(&name, &estimate) == nil {
				var exact int64
				query := `SELECT COUNT(*) FROM ` + pgx.Identifier{name}.Sanitize()
				if err := a.db.Pool().QueryRow(ctx, query).Scan(&exact); err != nil {
					exact = estimate
				}
				items = append(items, map[string]any{"name": name, "rows": exact})
			}
		}
		var databaseSize int64
		_ = a.db.Pool().QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&databaseSize)
		writeJSON(w, 200, map[string]any{"tables": items, "database_size_bytes": databaseSize})
		return
	}
	var allowed bool
	_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname='public' AND tablename=$1)`, table).Scan(&allowed)
	if !allowed {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "table not found"})
		return
	}
	query := `SELECT * FROM ` + pgx.Identifier{table}.Sanitize() + ` ORDER BY 1 DESC LIMIT 100`
	rows, err := a.db.Pool().Query(ctx, query)
	if err != nil {
		// Some tables may not have a sortable first column; fall back to physical order.
		query = `SELECT * FROM ` + pgx.Identifier{table}.Sanitize() + ` LIMIT 100`
		rows, err = a.db.Pool().Query(ctx, query)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "table preview unavailable"})
		return
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = string(f.Name)
	}
	data := make([]map[string]any, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			continue
		}
		row := make(map[string]any, len(columns))
		for i, value := range values {
			switch v := value.(type) {
			case []byte:
				row[columns[i]] = string(v)
			default:
				row[columns[i]] = v
			}
		}
		data = append(data, row)
	}
	writeJSON(w, 200, map[string]any{"table": table, "columns": columns, "rows": data, "limit": 100})
}

type windowsSystemSnapshot struct {
	Computer struct {
		Name                string `json:"Name"`
		Manufacturer        string `json:"Manufacturer"`
		Model               string `json:"Model"`
		TotalPhysicalMemory uint64 `json:"TotalPhysicalMemory"`
	} `json:"computer"`
	OS struct {
		Caption                string `json:"Caption"`
		Version                string `json:"Version"`
		OSArchitecture         string `json:"OSArchitecture"`
		TotalVisibleMemorySize uint64 `json:"TotalVisibleMemorySize"`
		FreePhysicalMemory     uint64 `json:"FreePhysicalMemory"`
		LastBootUpTime         string `json:"LastBootUpTime"`
	} `json:"os"`
	CPU struct {
		Name                      string `json:"Name"`
		LoadPercentage            uint16 `json:"LoadPercentage"`
		NumberOfCores             uint32 `json:"NumberOfCores"`
		NumberOfLogicalProcessors uint32 `json:"NumberOfLogicalProcessors"`
		MaxClockSpeed             uint32 `json:"MaxClockSpeed"`
	} `json:"cpu"`
	GPU         []map[string]any `json:"gpu"`
	Disk        []map[string]any `json:"disk"`
	Temperature []map[string]any `json:"temperature"`
}

func windowsSnapshot() (map[string]any, error) {
	command := `$ErrorActionPreference='SilentlyContinue';` +
		`$computer=Get-CimInstance Win32_ComputerSystem | Select-Object -First 1 Name,Manufacturer,Model,TotalPhysicalMemory;` +
		`$os=Get-CimInstance Win32_OperatingSystem | Select-Object -First 1 Caption,Version,OSArchitecture,TotalVisibleMemorySize,FreePhysicalMemory,LastBootUpTime;` +
		`$cpu=Get-CimInstance Win32_Processor | Select-Object -First 1 Name,LoadPercentage,NumberOfCores,NumberOfLogicalProcessors,MaxClockSpeed;` +
		`$gpu=@(Get-CimInstance Win32_VideoController | Select-Object Name,AdapterRAM,DriverVersion);` +
		`$disk=@(Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | Select-Object DeviceID,VolumeName,Size,FreeSpace);` +
		`$temperature=@();try{$temperature=@(Get-CimInstance -Namespace root/wmi -ClassName MSAcpi_ThermalZoneTemperature | Select-Object CurrentTemperature,InstanceName)}catch{};` +
		`$network=@(Get-NetAdapter | Where-Object {$_.Status -eq 'Up'} | Select-Object Name,InterfaceDescription,LinkSpeed,MacAddress,MediaType,PhysicalMediaType,Status);` +
		`$ip=@(Get-NetIPConfiguration | Where-Object {$_.IPv4Address} | Select-Object InterfaceAlias,@{N='IPv4';E={$_.IPv4Address.IPAddress}},@{N='Gateway';E={$_.IPv4DefaultGateway.NextHop}});` +
		`$usb=@(Get-PnpDevice -PresentOnly | Where-Object {$_.InstanceId -like 'USB*'} | Select-Object -First 50 FriendlyName,Class,Status,InstanceId);` +
		`[pscustomobject]@{computer=$computer;os=$os;cpu=$cpu;gpu=$gpu;disk=$disk;temperature=$temperature;network=$network;ip=$ip;usb=$usb}|ConvertTo-Json -Depth 6 -Compress`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("system inventory unavailable: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *appServer) developerSystem(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireDeveloper(w, r); !ok {
		return
	}
	host, _ := os.Hostname()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	payload := map[string]any{
		"host":        host,
		"runtime":     map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpu": runtime.NumCPU(), "go_version": runtime.Version(), "process_memory": mem.Alloc},
		"captured_at": time.Now().Format(time.RFC3339),
		"recent_logs": a.logs.snapshot(5000),
	}
	if runtime.GOOS == "windows" {
		if snapshot, err := windowsSnapshot(); err == nil {
			payload["system"] = snapshot
		} else {
			payload["system_error"] = "Some hardware readings are unavailable on this device."
		}
	}
	writeJSON(w, 200, payload)
}

func (a *appServer) developerSystemSocket(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireDeveloper(w, r); !ok {
		return
	}
	conn, err := a.logs.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.logs.mu.Lock()
	a.logs.clients[conn] = struct{}{}
	initial := append([]string(nil), a.logs.lines...)
	a.logs.mu.Unlock()
	_ = conn.WriteJSON(map[string]any{"type": "history", "lines": initial})
	defer func() {
		a.logs.mu.Lock()
		delete(a.logs.clients, conn)
		a.logs.mu.Unlock()
		_ = conn.Close()
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
