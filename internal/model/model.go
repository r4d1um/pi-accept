// Package model holds the types shared between checks, phases, ui and report.
// JSON fields are only ever added, never renamed (bump SchemaVersion on breaks).
package model

import "time"

const SchemaVersion = 1

type Status int

const (
	Pass Status = iota
	Info
	Warn
	Fail
	Skip
)

func (s Status) String() string {
	return [...]string{"PASS", "INFO", "WARN", "FAIL", "SKIP"}[s]
}

func (s Status) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Result is one check outcome. Hint explains the pitfall and the fix.
type Result struct {
	ID     string `json:"id"`
	Group  string `json:"group"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	Value  string `json:"value"`
	Hint   string `json:"hint,omitempty"`
}

// Sample is one reading taken during the stress phase.
type Sample struct {
	At        time.Time `json:"at"`
	TempC     float64   `json:"temp_c"`
	ArmMHz    float64   `json:"arm_mhz"`
	Throttled uint64    `json:"throttled"`
}

type Report struct {
	Schema   int       `json:"schema"`
	Version  string    `json:"version"`
	Host     string    `json:"host"`
	Model    string    `json:"model"`
	Started  time.Time `json:"started_utc"`
	Finished time.Time `json:"finished_utc"`
	Results  []Result  `json:"results"`
	Samples  []Sample  `json:"stress_samples,omitempty"`
	Verdict  Status    `json:"verdict"`
}

// Verdict is the worst status among results (Info/Skip don't count).
func Verdict(rs []Result) Status {
	v := Pass
	for _, r := range rs {
		if (r.Status == Warn || r.Status == Fail) && r.Status > v {
			v = r.Status
		}
	}
	return v
}
