package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

type runConfig struct {
	BaseURL        string
	TeamCount      int
	UsersPerTeam   int
	TargetsPerTeam int
	PRsPerTarget   int
	BatchSize      int
	RPS            float64
	Duration       time.Duration
	RequestTimeout time.Duration
	HealthTimeout  time.Duration
	ReportPath     string
	DatasetPrefix  string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
}

type datasetInfo struct {
	Prefix         string `json:"prefix"`
	TeamCount      int    `json:"team_count"`
	UsersPerTeam   int    `json:"users_per_team"`
	TargetsPerTeam int    `json:"targets_per_team"`
	PRsPerTarget   int    `json:"prs_per_target"`
	BatchSize      int    `json:"batch_size"`
}

type prTemplate struct {
	ID        string
	Reviewers []string
}

type teamScenario struct {
	Name        string
	Targets     []string
	NonTargets  []string
	PRTemplates []prTemplate
	lock        sync.Mutex
}

type teamTarget struct {
	Team    *teamScenario
	UserIDs []string
}

type latencySummary struct {
	Samples   int     `json:"samples"`
	AverageMs float64 `json:"average_ms"`
	P95Ms     float64 `json:"p95_ms"`
	MaxMs     float64 `json:"max_ms"`
}

type totalsSummary struct {
	Requested int `json:"requested"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type loadSummary struct {
	GeneratedAt time.Time      `json:"generated_at"`
	BaseURL     string         `json:"base_url"`
	DurationSec float64        `json:"duration_sec"`
	TargetRPS   float64        `json:"target_rps"`
	ActualRPS   float64        `json:"actual_rps"`
	Dataset     datasetInfo    `json:"dataset"`
	Totals      totalsSummary  `json:"totals"`
	BulkLatency latencySummary `json:"bulk_deactivate_latency_ms"`
	Reassign    latencySummary `json:"reassignment_latency_ms"`
	Errors      []string       `json:"errors,omitempty"`
}

type metricRecorder struct {
	mu                sync.Mutex
	total             int
	success           int
	failures          int
	durations         []time.Duration
	reassignDurations []time.Duration
	errors            []string
}

func (m *metricRecorder) record(duration time.Duration, reassignments int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total++
	if err != nil {
		m.failures++
		if len(m.errors) < 10 {
			m.errors = append(m.errors, err.Error())
		}
		return
	}
	m.success++
	m.durations = append(m.durations, duration)
	if reassignments > 0 {
		m.reassignDurations = append(m.reassignDurations, duration)
	}
}

func (m *metricRecorder) toSummary(elapsed time.Duration, cfg runConfig, data datasetInfo) loadSummary {
	summary := loadSummary{
		GeneratedAt: time.Now(),
		BaseURL:     cfg.BaseURL,
		DurationSec: elapsed.Seconds(),
		TargetRPS:   cfg.RPS,
		Dataset:     data,
		Totals: totalsSummary{
			Requested: m.total,
			Succeeded: m.success,
			Failed:    m.failures,
		},
		Errors: append([]string(nil), m.errors...),
	}
	if elapsed > 0 {
		summary.ActualRPS = float64(m.success) / elapsed.Seconds()
	}
	summary.BulkLatency = calcLatency(m.durations)
	summary.Reassign = calcLatency(m.reassignDurations)
	return summary
}

func calcLatency(data []time.Duration) latencySummary {
	if len(data) == 0 {
		return latencySummary{}
	}
	samples := append([]time.Duration(nil), data...)
	sort.Slice(samples, func(i, j int) bool {
		return samples[i] < samples[j]
	})

	var total time.Duration
	for _, d := range samples {
		total += d
	}
	avg := float64(total.Microseconds()) / float64(len(samples))
	maxDur := samples[len(samples)-1]
	p95 := samples[int(math.Ceil(0.95*float64(len(samples))))-1]
	return latencySummary{
		Samples:   len(samples),
		AverageMs: avg / 1000.0,
		P95Ms:     float64(p95.Microseconds()) / 1000.0,
		MaxMs:     float64(maxDur.Microseconds()) / 1000.0,
	}
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		log.Fatalf("load test failed: %v", err)
	}
}

func parseFlags() runConfig {
	var cfg runConfig
	flag.StringVar(&cfg.BaseURL, "base-url", "http://localhost:8080", "base URL of the running service")
	flag.IntVar(&cfg.TeamCount, "teams", 10, "number of teams to seed (<=20)")
	flag.IntVar(&cfg.UsersPerTeam, "users-per-team", 20, "users per team (<=200 total users)")
	flag.IntVar(&cfg.TargetsPerTeam, "targets-per-team", 8, "number of users per team participating in deactivate scenarios")
	flag.IntVar(&cfg.PRsPerTarget, "prs-per-target", 2, "number of open PRs seeded per target user")
	flag.IntVar(&cfg.BatchSize, "batch-size", 4, "number of users per deactivate request")
	flag.Float64Var(&cfg.RPS, "rps", 5, "target requests per second")
	flag.DurationVar(&cfg.Duration, "duration", 30*time.Second, "load duration (e.g. 45s, 1m)")
	flag.DurationVar(&cfg.RequestTimeout, "request-timeout", 2*time.Second, "HTTP request timeout")
	flag.DurationVar(&cfg.HealthTimeout, "health-timeout", 30*time.Second, "maximum wait for /health readiness")
	flag.StringVar(&cfg.ReportPath, "report", "tests/load/results/latest.json", "path to store structured results")
	flag.StringVar(&cfg.DatasetPrefix, "dataset-prefix", "load", "prefix for generated teams and PRs")

	flag.StringVar(&cfg.DBHost, "db-host", "localhost", "PostgreSQL host")
	flag.StringVar(&cfg.DBPort, "db-port", "5432", "PostgreSQL port")
	flag.StringVar(&cfg.DBUser, "db-user", "postgres", "PostgreSQL user")
	flag.StringVar(&cfg.DBPassword, "db-password", "secret", "PostgreSQL password")
	flag.StringVar(&cfg.DBName, "db-name", "prManagerDb", "PostgreSQL database name")

	flag.Parse()
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	totalUsers := cfg.TeamCount * cfg.UsersPerTeam
	if cfg.TeamCount <= 0 || cfg.TeamCount > 20 {
		log.Fatalf("teams must be within 1..20, got %d", cfg.TeamCount)
	}
	if totalUsers > 200 {
		log.Fatalf("total users %d exceed limit (200)", totalUsers)
	}
	if cfg.TargetsPerTeam <= 0 || cfg.TargetsPerTeam >= cfg.UsersPerTeam {
		log.Fatalf("targets-per-team must be between 1 and users-per-team-1")
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > cfg.TargetsPerTeam {
		log.Fatalf("batch-size must be between 1 and targets-per-team")
	}
	if cfg.PRsPerTarget <= 0 {
		log.Fatalf("prs-per-target must be positive")
	}
	if cfg.RPS <= 0 {
		log.Fatalf("rps must be positive")
	}
	return cfg
}

func run(cfg runConfig) error {
	rand.Seed(time.Now().UnixNano())

	ctx := context.Background()
	client := &http.Client{Timeout: cfg.RequestTimeout}

	if err := waitForHealthy(ctx, client, cfg.BaseURL, cfg.HealthTimeout); err != nil {
		return fmt.Errorf("service unhealthy: %w", err)
	}
	log.Printf("Service is healthy at %s", cfg.BaseURL)

	pool, err := pgxpool.New(ctx, cfg.dbConnString())
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	info := datasetInfo{
		TeamCount:      cfg.TeamCount,
		UsersPerTeam:   cfg.UsersPerTeam,
		TargetsPerTeam: cfg.TargetsPerTeam,
		PRsPerTarget:   cfg.PRsPerTarget,
		BatchSize:      cfg.BatchSize,
	}

	teams, err := seedDataset(ctx, client, pool, cfg)
	if err != nil {
		return fmt.Errorf("seed dataset: %w", err)
	}
	info.Prefix = cfg.DatasetPrefix
	if len(teams) > 0 {
		if idx := strings.LastIndex(teams[0].Name, "-team-"); idx > 0 {
			info.Prefix = teams[0].Name[:idx]
		} else {
			info.Prefix = teams[0].Name
		}
	}
	log.Printf("Seeded %d teams (%s...)", len(teams), info.Prefix)

	targets := buildTargets(teams, cfg.BatchSize)
	if len(targets) == 0 {
		return fmt.Errorf("no target combinations generated")
	}

	start := time.Now()
	recorder := &metricRecorder{}
	if err := executeLoad(ctx, client, pool, cfg, targets, recorder); err != nil {
		return err
	}
	elapsed := time.Since(start)
	summary := recorder.toSummary(elapsed, cfg, info)

	printSummary(summary)
	if err := writeReport(summary, cfg.ReportPath); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func (cfg runConfig) dbConnString() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.DBUser, cfg.DBPassword),
		Host:   net.JoinHostPort(cfg.DBHost, cfg.DBPort),
		Path:   cfg.DBName,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

func waitForHealthy(ctx context.Context, client *http.Client, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for health")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
}

func seedDataset(ctx context.Context, client *http.Client, pool *pgxpool.Pool, cfg runConfig) ([]*teamScenario, error) {
	prefix := fmt.Sprintf("%s-%d", cfg.DatasetPrefix, time.Now().Unix())
	var teams []*teamScenario
	for i := 0; i < cfg.TeamCount; i++ {
		teamName := fmt.Sprintf("%s-team-%02d", prefix, i+1)
		members, targets, nonTargets := buildMembers(teamName, cfg.UsersPerTeam, cfg.TargetsPerTeam)
		payload := models.Team{
			TeamName: teamName,
			Members:  members,
		}
		if err := postJSON(ctx, client, cfg.BaseURL+"/team/add", payload, http.StatusCreated); err != nil {
			return nil, fmt.Errorf("create team %s: %w", teamName, err)
		}
		prTemplates, err := seedPullRequests(ctx, pool, teamName, targets, nonTargets, cfg.PRsPerTarget)
		if err != nil {
			return nil, fmt.Errorf("seed PRs for %s: %w", teamName, err)
		}
		teams = append(teams, &teamScenario{
			Name:        teamName,
			Targets:     targets,
			NonTargets:  nonTargets,
			PRTemplates: prTemplates,
		})
	}
	return teams, nil
}

func buildMembers(teamName string, usersPerTeam, targetsPerTeam int) ([]models.TeamMember, []string, []string) {
	members := make([]models.TeamMember, 0, usersPerTeam)
	var targets []string
	var nonTargets []string
	for i := 0; i < usersPerTeam; i++ {
		userID := fmt.Sprintf("%s-user-%03d", teamName, i+1)
		member := models.TeamMember{
			UserId:   userID,
			Username: fmt.Sprintf("%s-user-%03d", teamName, i+1),
			IsActive: true,
		}
		members = append(members, member)
		if i < targetsPerTeam {
			targets = append(targets, userID)
		} else {
			nonTargets = append(nonTargets, userID)
		}
	}
	return members, targets, nonTargets
}

func seedPullRequests(ctx context.Context, pool *pgxpool.Pool, teamName string, targets, replacements []string, prsPerTarget int) ([]prTemplate, error) {
	if len(replacements) == 0 {
		return nil, fmt.Errorf("team %s must have at least one replacement user", teamName)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var templates []prTemplate
	var counter int
	for idx, target := range targets {
		for j := 0; j < prsPerTarget; j++ {
			counter++
			prID := fmt.Sprintf("%s-pr-%03d", teamName, counter)
			prName := fmt.Sprintf("%s feature %03d", teamName, counter)
			author := replacements[(idx+j)%len(replacements)]
			created := time.Now().Add(-time.Duration(counter) * time.Minute)
			if _, err := tx.Exec(ctx, `
				INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status, created_at, merged_at)
				VALUES ($1, $2, $3, 'OPEN', $4, NULL)
			`, prID, prName, author, created); err != nil {
				return nil, fmt.Errorf("insert pull request %s: %w", prID, err)
			}
			reviewerB := replacements[(idx+j+1)%len(replacements)]
			reviewers := []string{target, reviewerB}
			for _, reviewer := range reviewers {
				if _, err := tx.Exec(ctx, `
					INSERT INTO pull_request_reviewers (pull_request_id, user_id)
					VALUES ($1, $2)
				`, prID, reviewer); err != nil {
					return nil, fmt.Errorf("insert reviewer %s for %s: %w", reviewer, prID, err)
				}
			}
			templates = append(templates, prTemplate{
				ID:        prID,
				Reviewers: reviewers,
			})
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return templates, nil
}

func buildTargets(teams []*teamScenario, batchSize int) []teamTarget {
	var targets []teamTarget
	for _, team := range teams {
		if len(team.Targets) < batchSize {
			continue
		}
		for i := 0; i+batchSize <= len(team.Targets); i++ {
			users := append([]string(nil), team.Targets[i:i+batchSize]...)
			targets = append(targets, teamTarget{
				Team:    team,
				UserIDs: users,
			})
		}
	}
	return targets
}

func executeLoad(ctx context.Context, client *http.Client, pool *pgxpool.Pool, cfg runConfig, targets []teamTarget, recorder *metricRecorder) error {
	interval := time.Duration(float64(time.Second) / cfg.RPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	totalRequests := int(math.Round(cfg.Duration.Seconds() * cfg.RPS))
	if totalRequests == 0 {
		totalRequests = int(cfg.Duration.Seconds()) + 1
	}

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		<-ticker.C
		target := targets[rand.Intn(len(targets))]
		wg.Add(1)
		go func(tt teamTarget) {
			defer wg.Done()
			duration, reassignments, err := executeRequest(ctx, client, pool, cfg.BaseURL, tt)
			recorder.record(duration, reassignments, err)
		}(target)
	}
	wg.Wait()
	return nil
}

func executeRequest(ctx context.Context, client *http.Client, pool *pgxpool.Pool, baseURL string, target teamTarget) (time.Duration, int, error) {
	target.Team.lock.Lock()
	defer target.Team.lock.Unlock()

	if err := resetTeamState(ctx, pool, target.Team); err != nil {
		return 0, 0, fmt.Errorf("reset team %s: %w", target.Team.Name, err)
	}

	payload := models.TeamBulkDeactivateRequest{
		TeamName: target.Team.Name,
		UserIDs:  target.UserIDs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/team/deactivateUsers", bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Result *models.TeamBulkDeactivateResult `json:"result"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, 0, fmt.Errorf("decode response: %w", err)
	}
	duration := time.Since(start)
	reassignCount := 0
	if parsed.Result != nil {
		reassignCount = len(parsed.Result.Reassignments)
	}
	return duration, reassignCount, nil
}

func resetTeamState(ctx context.Context, pool *pgxpool.Pool, team *teamScenario) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE users SET is_active = true WHERE team_name = $1`, team.Name); err != nil {
		return fmt.Errorf("reactivate team %s users: %w", team.Name, err)
	}

	for _, pr := range team.PRTemplates {
		if _, err := tx.Exec(ctx, `DELETE FROM pull_request_reviewers WHERE pull_request_id = $1`, pr.ID); err != nil {
			return fmt.Errorf("cleanup reviewers for %s: %w", pr.ID, err)
		}
		for _, reviewer := range pr.Reviewers {
			if _, err := tx.Exec(ctx, `
				INSERT INTO pull_request_reviewers (pull_request_id, user_id)
				VALUES ($1, $2)
			`, pr.ID, reviewer); err != nil {
				return fmt.Errorf("restore reviewer %s for %s: %w", reviewer, pr.ID, err)
			}
		}
	}

	return tx.Commit(ctx)
}

func postJSON(ctx context.Context, client *http.Client, url string, payload interface{}, expectedStatus int) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func printSummary(summary loadSummary) {
	fmt.Printf("\nLoad test summary:\n")
	fmt.Printf("  Target RPS: %.2f, Actual RPS: %.2f\n", summary.TargetRPS, summary.ActualRPS)
	fmt.Printf("  Requests: %d total, %d succeeded, %d failed\n", summary.Totals.Requested, summary.Totals.Succeeded, summary.Totals.Failed)
	fmt.Printf("  Bulk deactivate latency avg: %.2f ms, p95: %.2f ms, max: %.2f ms\n",
		summary.BulkLatency.AverageMs, summary.BulkLatency.P95Ms, summary.BulkLatency.MaxMs)
	fmt.Printf("  Reassignment latency avg: %.2f ms, p95: %.2f ms, max: %.2f ms\n",
		summary.Reassign.AverageMs, summary.Reassign.P95Ms, summary.Reassign.MaxMs)
	if len(summary.Errors) > 0 {
		fmt.Println("  Sample errors:")
		for _, err := range summary.Errors {
			fmt.Printf("   - %s\n", err)
		}
	}
}

func writeReport(summary loadSummary, path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
