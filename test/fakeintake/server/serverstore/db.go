// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2024-present Datadog, Inc.

package serverstore

import (
	"database/sql"
	"log"
	"os"
	"time"

	"github.com/DataDog/datadog-agent/test/fakeintake/api"

	// import sqlite3 driver
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultDBPath = "payloads.db"

	metricsTicker = 30 * time.Second
)

// SQLStore implements a thread-safe storage for raw and json dumped payloads using SQLite
type SQLStore struct {
	db   *sql.DB
	path string

	stopCh  chan struct{}
	metrics sqlMetrics
}

type sqlMetrics struct {
	// nBPayloads is a prometheus metric to track the number of payloads collected by route
	nBPayloads *prometheus.GaugeVec
	// insertLatency is a prometheus metric to track the latency of inserting payloads
	insertLatency *prometheus.HistogramVec
	// ReadLatency is a prometheus metric to track the latency of reading payloads
	readLatency *prometheus.HistogramVec
}

// NewSQLStore initializes a new payloads store with an SQLite DB
func NewSQLStore() *SQLStore {
	p := os.Getenv("SQLITE_DB_PATH")
	if p == "" {
		f, err := os.CreateTemp("", defaultDBPath)
		if err != nil {
			log.Fatal(err)
		}
		p = f.Name()
	}
	db, err := sql.Open("sqlite3", p)
	if err != nil {
		log.Fatal(err)
	}

	// Enable WAL mode for better performances
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		log.Fatal("Failed to enable WAL mode: ", err)
	}

	s := &SQLStore{
		path:   p,
		db:     db,
		stopCh: make(chan struct{}),

		metrics: sqlMetrics{
			nBPayloads: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "payloads",
				Help: "Number of payloads collected by route",
			}, []string{"route"}),
			insertLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "insert_latency",
				Help:    "Latency of inserting payloads",
				Buckets: prometheus.DefBuckets,
			}, []string{"route"}),
			readLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "read_latency",
				Help:    "Latency of reading payloads",
				Buckets: prometheus.DefBuckets,
			}, []string{"route"}),
		},
	}

	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS payloads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		data BLOB NOT NULL,
		encoding VARCHAR(10) NOT NULL,
		route VARCHAR(20) NOT NULL
	);
	`)

	if err != nil {
		log.Fatal("Failed to ensure table creation: ", err)
	}

	go func() {
		ticker := time.NewTicker(metricsTicker)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				routes, err := s.db.Query("SELECT route, COUNT(*) FROM payloads GROUP BY route")
				if err != nil {
					log.Println("Error fetching route stats: ", err)
					continue
				}
				defer routes.Close()
				for routes.Next() {
					var route string
					var count int
					if err := routes.Scan(&route, &count); err != nil {
						log.Println("Error scanning route stat: ", err)
						continue
					}
					s.metrics.nBPayloads.WithLabelValues(route).Set(float64(count))
				}
			}
		}
	}()

	return s
}

// Close closes the store
func (s *SQLStore) Close() {
	s.db.Close()
	s.stopCh <- struct{}{}
	os.Remove(s.path)
}

// AppendPayload adds a payload to the store and tries parsing and adding a dumped json to the parsed store
func (s *SQLStore) AppendPayload(route string, data []byte, encoding string, collectTime time.Time) error {
	now := time.Now()
	_, err := s.db.Exec("INSERT INTO payloads (timestamp, data, encoding, route) VALUES (?, ?, ?, ?)", collectTime.Unix(), data, encoding, route)
	if err != nil {
		return err
	}
	obs := time.Since(now).Seconds()
	s.metrics.insertLatency.WithLabelValues(route).Observe(obs)
	log.Printf("Inserted payload for route %s in %f seconds\n", route, obs)

	rawPayload := api.Payload{
		Timestamp: collectTime,
		Data:      data,
		Encoding:  encoding,
	}

	return s.tryParseAndAppendPayload(rawPayload, route)
}

func (s *SQLStore) tryParseAndAppendPayload(rawPayload api.Payload, route string) error {
	parsedPayload, err := tryParse(rawPayload, route)
	if err != nil {
		return err
	}
	if parsedPayload == nil {
		return nil
	}

	return nil
}

// CleanUpPayloadsOlderThan removes payloads older than specified time
func (s *SQLStore) CleanUpPayloadsOlderThan(time time.Time) {
	log.Printf("Cleaning up payloads")
	_, err := s.db.Exec("DELETE FROM payloads WHERE timestamp < ?", time.Unix())
	if err != nil {
		log.Println("Error cleaning payloads: ", err)
	}

	routes, err := s.db.Query("SELECT DISTINCT route FROM payloads")
	if err != nil {
		log.Println("Error fetching distinct routes: ", err)
		return
	}
	defer routes.Close()

	for routes.Next() {
		var route string
		if err := routes.Scan(&route); err != nil {
			log.Println("Error scanning route: ", err)
			continue
		}
	}
}

// GetRawPayloads returns all raw payloads for a given route
func (s *SQLStore) GetRawPayloads(route string) []api.Payload {
	now := time.Now()
	rows, err := s.db.Query("SELECT timestamp, data, encoding FROM payloads WHERE route = ?", route)
	if err != nil {
		log.Println("Error fetching raw payloads: ", err)
		return nil
	}
	defer rows.Close()
	s.metrics.readLatency.WithLabelValues(route).Observe(time.Since(now).Seconds())

	var timestamp int64
	var data []byte
	var encoding string
	payloads := []api.Payload{}
	for rows.Next() {
		err := rows.Scan(&timestamp, &data, &encoding)
		if err != nil {
			log.Println("Error scanning raw payload: ", err)
			continue
		}
		payloads = append(payloads, api.Payload{
			Timestamp: time.Unix(timestamp, 0),
			Data:      data,
			Encoding:  encoding,
		})
	}
	return payloads
}

// GetJSONPayloads returns all parsed payloads for a given route
func (s *SQLStore) GetJSONPayloads(route string) (payloads []api.ParsedPayload) {
	return nil
}

// GetRouteStats returns the number of payloads for each route
func (s *SQLStore) GetRouteStats() (statsByRoute map[string]int) {
	statsByRoute = make(map[string]int)
	rows, err := s.db.Query("SELECT route, COUNT(*) FROM payloads GROUP BY route")
	if err != nil {
		log.Println("Error fetching route stats: ", err)
		return
	}
	defer rows.Close()

	var route string
	var count int
	for rows.Next() {
		err := rows.Scan(&route, &count)
		if err != nil {
			log.Println("Error scanning route stat: ", err)
			continue
		}
		statsByRoute[route] = count
	}
	return statsByRoute
}

// Flush flushes the store
func (s *SQLStore) Flush() {
	_, err := s.db.Exec("DELETE FROM payloads")
	if err != nil {
		log.Println("Error flushing payloads: ", err)
	}
}

// GetMetrics returns the prometheus metrics for the store
func (s *SQLStore) GetMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		s.metrics.nBPayloads,
		s.metrics.insertLatency,
		s.metrics.readLatency,
	}
}
