package api

import (
	"net/http"
	"strconv"

	"github.com/deepsea-ops/server/internal/audit"
	"github.com/deepsea-ops/server/internal/auth"
)

// handleListAuditLogs GET /api/audit-logs?username=&action=&target=&start=&end=&offset=&limit=
// 返回 {total, items}, 最新优先。
func handleListAuditLogs(w http.ResponseWriter, r *http.Request, aud *audit.Store) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	f := audit.Filter{
		Username: q.Get("username"),
		Action:   q.Get("action"),
		Target:   q.Get("target"),
	}
	if s := q.Get("start"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			f.Start = v
		}
	}
	if e := q.Get("end"); e != "" {
		if v, err := strconv.ParseInt(e, 10, 64); err == nil {
			f.End = v
		}
	}
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			f.Offset = v
		}
	}
	f.Limit = 50
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500 {
			f.Limit = v
		}
	}

	logs, total, err := aud.Query(f)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"total": total,
		"items": logs,
	})
}
