package main

import (
	"database/sql"
	"fmt"
	"math"
	"net/netip"
	"os"
	"time"

	"github.com/mdlayher/arp"
	_ "modernc.org/sqlite"
)

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS daily (
			date TEXT NOT NULL,
			mac  TEXT NOT NULL,
			cnt  INTEGER DEFAULT 0,
			PRIMARY KEY (date, mac)
		);
		CREATE TABLE IF NOT EXISTS aliases (
			mac  TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL
		);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func saveResults(db *sql.DB, t time.Time, devices []device) error {
	date := t.Format("2006-01-02")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO daily (date, mac, cnt) VALUES (?, ?, 1)
		ON CONFLICT(date, mac) DO UPDATE SET cnt = cnt + 1`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, d := range devices {
		if _, err := stmt.Exec(date, d.MAC.String()); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func cleanupOldData(db *sql.DB) {
	sixMonthsAgo := time.Now().AddDate(0, -6, 0).Format("2006-01-02")
	result, err := db.Exec("DELETE FROM daily WHERE date < ?", sixMonthsAgo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "清理旧数据失败: %v\n", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		fmt.Printf("清理了 %d 条半年前的数据\n", n)
	}
}

func generateMonthlyReport(db *sql.DB, t time.Time) string {
	year, month, _ := t.Date()
	start := time.Date(year, month, 1, 0, 0, 0, 0, t.Location())
	end := start.AddDate(0, 1, -1)
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")

	var deviceCount, dayCount int
	db.QueryRow(`SELECT COUNT(DISTINCT mac), COUNT(DISTINCT date) FROM daily WHERE date BETWEEN ? AND ?`,
		startStr, endStr).Scan(&deviceCount, &dayCount)

	s := fmt.Sprintf("# 月度报告 - %d年%d月\n\n统计周期: %s ~ %s\n\n## 汇总\n- 本月在线设备: %d 台\n- 覆盖天数: %d 天\n\n## 详细排行\n\n| 排名 | 姓名 | 在线天数 | 总时长 |\n|------|------|---------|--------|\n",
		year, month, startStr, endStr, deviceCount, dayCount)

	rows, err := db.Query(`
		SELECT a.name,
		       COUNT(DISTINCT d.date) as days,
		       CAST(SUM(d.cnt) * 5 AS REAL) / 60.0 as hours
		FROM daily d
		INNER JOIN aliases a ON d.mac = a.mac
		WHERE d.date BETWEEN ? AND ?
		GROUP BY d.mac
		ORDER BY hours DESC
	`, startStr, endStr)
	if err != nil {
		return s
	}
	defer rows.Close()

	rank := 1
	for rows.Next() {
		var name string
		var days int
		var hours float64
		if err := rows.Scan(&name, &days, &hours); err != nil {
			continue
		}
		total := math.Round(hours * 60)
		hh := int(total) / 60
		mm := int(total) % 60
		dur := fmt.Sprintf("%dh", hh)
		if mm > 0 {
			dur += fmt.Sprintf("%dm", mm)
		}
		s += fmt.Sprintf("| %d | %s | %d | %s |\n", rank, name, days, dur)
		rank++
	}

	if rank == 1 {
		s += "（无已注册设备的记录）\n"
	}

	return s
}

func runDaemon(client *arp.Client, localIP netip.Addr, targets []netip.Addr, db *sql.DB, state *ScannerState) {
	fmt.Println("ARP 守护模式启动")
	fmt.Println("扫描间隔: 5 分钟")
	fmt.Println("记录时段: 06:00 ~ 24:00")
	fmt.Println()

	var lastCleanup time.Time
	var lastReportMonth time.Month

	for {
		now := time.Now()
		inWindow := now.Hour() >= 6 && now.Hour() < 24

		if now.Day() == 1 && lastReportMonth != now.Month() {
			report := generateMonthlyReport(db, now.AddDate(0, -1, 0))
			fmt.Println(report)
			lastReportMonth = now.Month()
		}

		if time.Since(lastCleanup) > 24*time.Hour {
			cleanupOldData(db)
			lastCleanup = time.Now()
		}

		hosts := makeHosts(targets)
		results := scan(client, localIP, hosts)

		state.Update(results)

		if inWindow {
			if err := saveResults(db, now, results); err != nil {
				fmt.Fprintf(os.Stderr, "[%s] 写入失败: %v\n", now.Format("15:04:05"), err)
			}
		}

		label := "✓"
		if !inWindow {
			label = "–"
		}
		fmt.Printf("[%s] %s %d 台设备 [%v]\n",
			now.Format("15:04:05"), label, len(results),
			time.Since(now).Truncate(10*time.Millisecond))

		next := time.Now().Truncate(5 * time.Minute).Add(5 * time.Minute)
		time.Sleep(time.Until(next))
	}
}

func makeHosts(targets []netip.Addr) []*arpHost {
	hosts := make([]*arpHost, len(targets))
	for i, ip := range targets {
		hosts[i] = &arpHost{ip: ip, nextSend: time.Now()}
	}
	return hosts
}
