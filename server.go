package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	_ "embed"
)

//go:embed index.html
var indexHTML string

type ScannerState struct {
	mu     sync.RWMutex
	online []string
}

func (s *ScannerState) Update(devices []device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.online = make([]string, len(devices))
	for i, d := range devices {
		s.online[i] = d.MAC.String()
	}
}

func (s *ScannerState) GetOnline() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := make([]string, len(s.online))
	copy(r, s.online)
	return r
}

func todaySlots() int {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, now.Location())
	if now.Before(start) {
		return 0
	}
	return int(now.Sub(start) / (5 * time.Minute))
}

func startServer(db *sql.DB, state *ScannerState) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(indexHTML))
	})
	mux.HandleFunc("/api/status", handleStatus(state))
	mux.HandleFunc("/api/daily", handleDaily(db))
	mux.HandleFunc("/api/monthly", handleMonthly(db))
	mux.HandleFunc("/api/aliases", handleAliases(db))

	log.Printf("HTTP 服务器启动于 :9527")
	if err := http.ListenAndServe(":9527", mux); err != nil {
		log.Fatalf("HTTP 服务器错误: %v", err)
	}
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func handleStatus(state *ScannerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{
			"slots":  todaySlots(),
			"online": state.GetOnline(),
		})
	}
}

func handleDaily(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}

		rows, err := db.Query(`
			SELECT d.mac, COALESCE(a.name, '') as name, d.cnt,
			       CAST(d.cnt * 5 AS REAL) / 60.0 as hours
			FROM daily d
			LEFT JOIN aliases a ON d.mac = a.mac
			WHERE d.date = ?
			ORDER BY d.cnt DESC
		`, date)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		defer rows.Close()

		type dev struct {
			MAC   string  `json:"mac"`
			Name  string  `json:"name"`
			Cnt   int     `json:"cnt"`
			Hours float64 `json:"hours"`
		}
		var devices []dev
		for rows.Next() {
			var d dev
			if err := rows.Scan(&d.MAC, &d.Name, &d.Cnt, &d.Hours); err != nil {
				jsonErr(w, 500, err.Error())
				return
			}
			devices = append(devices, d)
		}
		if devices == nil {
			devices = []dev{}
		}

		jsonOK(w, map[string]any{
			"date":    date,
			"slots":   todaySlots(),
			"devices": devices,
		})
	}
}

func handleMonthly(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ym := r.URL.Query().Get("ym")
		now := time.Now()

		if ym == "" {
			ym = now.Format("2006-01")
		}

		t, err := time.Parse("2006-01", ym)
		if err != nil {
			jsonErr(w, 400, "无效的月份格式, 期望 YYYY-MM")
			return
		}

		start := t.Format("2006-01-02")
		end := t.AddDate(0, 1, -1).Format("2006-01-02")
		days := now.Sub(t).Hours()/24 + 1

		rows, err := db.Query(`
			SELECT d.mac, COALESCE(a.name, '') as name,
			       COUNT(DISTINCT d.date) as days,
			       CAST(SUM(d.cnt) * 5 AS REAL) / 60.0 as hours
			FROM daily d
			LEFT JOIN aliases a ON d.mac = a.mac
			WHERE d.date BETWEEN ? AND ?
			GROUP BY d.mac
			ORDER BY hours DESC
		`, start, end)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		defer rows.Close()

		type dev struct {
			MAC   string  `json:"mac"`
			Name  string  `json:"name"`
			Days  int     `json:"days"`
			Hours float64 `json:"hours"`
		}
		var devices []dev
		for rows.Next() {
			var d dev
			if err := rows.Scan(&d.MAC, &d.Name, &d.Days, &d.Hours); err != nil {
				jsonErr(w, 500, err.Error())
				return
			}
			devices = append(devices, d)
		}
		if devices == nil {
			devices = []dev{}
		}

		jsonOK(w, map[string]any{
			"ym":      ym,
			"days":    int(days),
			"devices": devices,
		})
	}
}

func handleAliases(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rows, err := db.Query("SELECT mac, name FROM aliases ORDER BY name")
			if err != nil {
				jsonErr(w, 500, err.Error())
				return
			}
			defer rows.Close()

			type alias struct {
				MAC  string `json:"mac"`
				Name string `json:"name"`
			}
			var list []alias
			for rows.Next() {
				var a alias
				if err := rows.Scan(&a.MAC, &a.Name); err != nil {
					jsonErr(w, 500, err.Error())
					return
				}
				list = append(list, a)
			}
			if list == nil {
				list = []alias{}
			}
			jsonOK(w, list)

		case http.MethodPost:
			var body struct {
				MAC  string `json:"mac"`
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				jsonErr(w, 400, "无效的 JSON")
				return
			}
			if body.MAC == "" || body.Name == "" {
				jsonErr(w, 400, "mac 和 name 不能为空")
				return
			}
			_, err := db.Exec("INSERT INTO aliases (mac, name) VALUES (?, ?) ON CONFLICT(mac) DO UPDATE SET name = ?", body.MAC, body.Name, body.Name)
			if err != nil {
				jsonErr(w, 500, err.Error())
				return
			}
			jsonOK(w, map[string]string{"status": "ok"})

		case http.MethodDelete:
			mac := r.URL.Query().Get("mac")
			if mac == "" {
				jsonErr(w, 400, "缺少 mac 参数")
				return
			}
			_, err := db.Exec("DELETE FROM aliases WHERE mac = ?", mac)
			if err != nil {
				jsonErr(w, 500, err.Error())
				return
			}
			jsonOK(w, map[string]string{"status": "ok"})

		default:
			jsonErr(w, 405, "不支持的方法")
		}
	}
}
