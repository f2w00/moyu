package main

import (
	"database/sql"
	"fmt"
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

func runDaemon(client *arp.Client, localIP netip.Addr, targets []netip.Addr, db *sql.DB, state *ScannerState) {
	fmt.Println("ARP 守护模式启动")
	fmt.Println("扫描间隔: 5 分钟")
	fmt.Println("记录时段: 06:00 ~ 24:00")
	fmt.Println()

	for {
		now := time.Now()
		inWindow := now.Hour() >= 6 && now.Hour() < 24

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
