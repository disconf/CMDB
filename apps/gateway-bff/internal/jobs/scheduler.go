package jobs

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const scheduleTimeLayout = "2006-01-02 15:04:05"

type cronField struct {
	values   []int
	wildcard bool
}

type cronSchedule struct {
	minute cronField
	hour   cronField
	day    cronField
	month  cronField
	week   cronField
}

func scheduleLocation() *time.Location {
	name := strings.TrimSpace(os.Getenv("CMDB_SCHEDULE_TIMEZONE"))
	if name == "" {
		name = "Asia/Shanghai"
	}
	if location, err := time.LoadLocation(name); err == nil {
		return location
	}
	return time.FixedZone("CST", 8*60*60)
}

func scheduleID() string { return fmt.Sprintf("sch-%d", time.Now().UnixNano()) }

func locationForTimezone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrValidation
	}
	return location, nil
}

func scheduleLocationFor(schedule Schedule) *time.Location {
	location, err := locationForTimezone(schedule.Timezone)
	if err != nil {
		return scheduleLocation()
	}
	return location
}

func formatScheduleTimeIn(location *time.Location, value time.Time) string {
	if value.IsZero() {
		return ""
	}
	if location == nil {
		location = scheduleLocation()
	}
	return value.In(location).Format(scheduleTimeLayout)
}

func nextScheduleRuns(expression, timezone string, count int, from time.Time) ([]string, error) {
	location, err := locationForTimezone(timezone)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}
	cursor := from.In(location)
	result := make([]string, 0, count)
	for len(result) < count {
		next, nextErr := nextCronTime(expression, cursor)
		if nextErr != nil {
			return nil, ErrValidation
		}
		result = append(result, formatScheduleTimeIn(location, next))
		cursor = next
	}
	return result, nil
}

func (s *Service) PreviewSchedule(expression, timezone string, count int) ([]string, error) {
	return nextScheduleRuns(strings.TrimSpace(expression), strings.TrimSpace(timezone), count, time.Now())
}

func (s *Service) templateByID(id string) (Template, error) {
	s.mu.RLock()
	for _, item := range s.templates {
		if item.ID == id {
			s.mu.RUnlock()
			return item, nil
		}
	}
	s.mu.RUnlock()
	s.refreshTemplates()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.templates {
		if item.ID == id {
			return item, nil
		}
	}
	return Template{}, ErrNotFound
}

func (s *Service) scheduleTarget(input ScheduleInput) (Template, Playbook, error) {
	if strings.TrimSpace(input.PlaybookID) != "" {
		playbook, err := s.playbookByID(strings.TrimSpace(input.PlaybookID))
		if err != nil {
			return Template{}, Playbook{}, err
		}
		return Template{ID: "", Name: playbook.Name, Enabled: playbook.Enabled}, playbook, nil
	}
	template, err := s.templateByID(strings.TrimSpace(input.TemplateID))
	if err != nil {
		return Template{}, Playbook{}, err
	}
	return template, Playbook{}, nil
}
func normalizeScheduleInput(input ScheduleInput, template Template) (ScheduleInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.PlaybookID = strings.TrimSpace(input.PlaybookID)
	input.CredentialID = strings.TrimSpace(input.CredentialID)
	input.Cron = strings.TrimSpace(input.Cron)
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.ConcurrencyPolicy = strings.ToLower(strings.TrimSpace(input.ConcurrencyPolicy))
	input.MissedPolicy = strings.ToLower(strings.TrimSpace(input.MissedPolicy))
	input.RetryMax, input.RetryIntervalSeconds = normalizeRetryPolicy(input.RetryMax, input.RetryIntervalSeconds)
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if _, err := locationForTimezone(input.Timezone); err != nil {
		return input, ErrValidation
	}
	if input.ConcurrencyPolicy == "" {
		input.ConcurrencyPolicy = "forbid"
	}
	if input.ConcurrencyPolicy != "allow" && input.ConcurrencyPolicy != "forbid" && input.ConcurrencyPolicy != "replace" {
		return input, ErrValidation
	}
	if input.MissedPolicy == "" {
		input.MissedPolicy = "skip"
	}
	if input.MissedPolicy != "skip" && input.MissedPolicy != "run" {
		return input, ErrValidation
	}
	if input.Name == "" || (input.TemplateID == "" && input.PlaybookID == "") || input.Cron == "" || len(input.Targets) == 0 || !template.Enabled {
		return input, ErrValidation
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 300
	}
	if input.TimeoutSeconds > 86400 {
		return input, ErrValidation
	}
	if err := validateCron(input.Cron); err != nil {
		return input, ErrValidation
	}
	seen := map[string]bool{}
	targets := make([]string, 0, len(input.Targets))
	for _, target := range input.Targets {
		target = strings.TrimSpace(target)
		if target != "" && !seen[target] {
			seen[target] = true
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return input, ErrValidation
	}
	input.Targets = targets
	if input.Enabled == nil {
		enabled := true
		input.Enabled = &enabled
	}
	return input, nil
}

func (s *Service) CreateSchedule(input ScheduleInput, createdBy string) (Schedule, error) {
	template, playbook, err := s.scheduleTarget(input)
	if err != nil {
		return Schedule{}, ErrValidation
	}
	input, err = normalizeScheduleInput(input, template)
	if err != nil {
		return Schedule{}, err
	}
	location, _ := locationForTimezone(input.Timezone)
	next, err := nextCronTime(input.Cron, time.Now().In(location))
	if err != nil {
		return Schedule{}, ErrValidation
	}
	schedule := Schedule{ID: scheduleID(), Name: input.Name, TemplateID: template.ID, TemplateName: template.Name, PlaybookID: playbook.ID, PlaybookName: playbook.Name, Variables: cloneStringMap(input.Variables), Parameters: cloneStringMap(input.Parameters), Targets: append([]string(nil), input.Targets...), Cron: input.Cron, Timezone: input.Timezone, ConcurrencyPolicy: input.ConcurrencyPolicy, MissedPolicy: input.MissedPolicy, TimeoutSeconds: input.TimeoutSeconds, RetryMax: input.RetryMax, RetryIntervalSeconds: input.RetryIntervalSeconds, CredentialID: input.CredentialID, Enabled: *input.Enabled, NextRun: formatScheduleTimeIn(location, next), CreatedBy: createdBy}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules = append([]Schedule{schedule}, s.schedules...)
	if s.db != nil {
		if err := upsertSchedule(context.Background(), s.db, schedule); err != nil {
			return Schedule{}, err
		}
	}
	return cloneSchedule(schedule), nil
}
func (s *Service) UpdateSchedule(id string, input ScheduleInput) (Schedule, error) {
	template, playbook, err := s.scheduleTarget(input)
	if err != nil {
		return Schedule{}, ErrValidation
	}
	input, err = normalizeScheduleInput(input, template)
	if err != nil {
		return Schedule{}, err
	}
	location, _ := locationForTimezone(input.Timezone)
	next, err := nextCronTime(input.Cron, time.Now().In(location))
	if err != nil {
		return Schedule{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID != id {
			continue
		}
		s.schedules[i].Name = input.Name
		s.schedules[i].TemplateID = template.ID
		s.schedules[i].TemplateName = template.Name
		s.schedules[i].PlaybookID = playbook.ID
		s.schedules[i].PlaybookName = playbook.Name
		s.schedules[i].Variables = cloneStringMap(input.Variables)
		s.schedules[i].Parameters = cloneStringMap(input.Parameters)
		s.schedules[i].Targets = append([]string(nil), input.Targets...)
		s.schedules[i].Cron = input.Cron
		s.schedules[i].Timezone = input.Timezone
		s.schedules[i].ConcurrencyPolicy = input.ConcurrencyPolicy
		s.schedules[i].MissedPolicy = input.MissedPolicy
		s.schedules[i].TimeoutSeconds = input.TimeoutSeconds
		s.schedules[i].RetryMax = input.RetryMax
		s.schedules[i].RetryIntervalSeconds = input.RetryIntervalSeconds
		s.schedules[i].CredentialID = input.CredentialID
		s.schedules[i].Enabled = *input.Enabled
		if s.schedules[i].Enabled {
			s.schedules[i].NextRun = formatScheduleTimeIn(location, next)
		} else {
			s.schedules[i].NextRun = ""
		}
		if s.db != nil {
			if err := upsertSchedule(context.Background(), s.db, s.schedules[i]); err != nil {
				return Schedule{}, err
			}
		}
		return cloneSchedule(s.schedules[i]), nil
	}
	return Schedule{}, ErrNotFound
}
func (s *Service) ToggleSchedule(id string) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID != id {
			continue
		}
		s.schedules[i].Enabled = !s.schedules[i].Enabled
		if s.schedules[i].Enabled {
			location := scheduleLocationFor(s.schedules[i])
			next, err := nextCronTime(s.schedules[i].Cron, time.Now().In(location))
			if err != nil {
				s.schedules[i].Enabled = false
				return Schedule{}, ErrValidation
			}
			s.schedules[i].NextRun = formatScheduleTimeIn(location, next)
		} else {
			s.schedules[i].NextRun = ""
		}
		if s.db != nil {
			if err := upsertSchedule(context.Background(), s.db, s.schedules[i]); err != nil {
				return Schedule{}, err
			}
		}
		return cloneSchedule(s.schedules[i]), nil
	}
	return Schedule{}, ErrNotFound
}

func (s *Service) DeleteSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID != id {
			continue
		}
		s.schedules = append(s.schedules[:i], s.schedules[i+1:]...)
		if s.db != nil {
			return deleteScheduleRow(context.Background(), s.db, id)
		}
		return nil
	}
	return ErrNotFound
}

func (s *Service) scheduleByID(id string) (Schedule, error) {
	s.refreshSchedules()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.schedules {
		if item.ID == id {
			return cloneSchedule(item), nil
		}
	}
	return Schedule{}, ErrNotFound
}

func (s *Service) lockSchedule(id string) (func(), error) {
	if s.db == nil {
		return func() {}, nil
	}
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lockName := "job-schedule:" + id
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1)::bigint)`, lockName); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext($1)::bigint)`, lockName)
		_ = conn.Close()
	}, nil
}

func scheduleJobActive(status string) bool {
	return status == "pending" || status == "running" || status == "awaiting_approval"
}
func (s *Service) setScheduleNextRun(id, nextRun string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID != id {
			continue
		}
		s.schedules[i].NextRun = nextRun
		if s.db != nil {
			return upsertSchedule(context.Background(), s.db, s.schedules[i])
		}
		return nil
	}
	return ErrNotFound
}

func (s *Service) recordScheduleResult(id, status string, advance bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID != id {
			continue
		}
		location := scheduleLocationFor(s.schedules[i])
		now := time.Now().In(location)
		if status != "missed_skipped" {
			s.schedules[i].LastRun = now.Format(scheduleTimeLayout)
		}
		s.schedules[i].LastStatus = status
		if advance {
			next, err := nextCronTime(s.schedules[i].Cron, now)
			if err != nil {
				return ErrValidation
			}
			s.schedules[i].NextRun = formatScheduleTimeIn(location, next)
		}
		if s.db != nil {
			return upsertSchedule(context.Background(), s.db, s.schedules[i])
		}
		return nil
	}
	return ErrNotFound
}

func (s *Service) RunSchedule(id, operator string, advance bool) (Job, error) {
	unlock, err := s.lockSchedule(id)
	if err != nil {
		return Job{}, err
	}
	defer unlock()

	schedule, err := s.scheduleByID(id)
	if err != nil {
		return Job{}, err
	}
	if advance {
		location := scheduleLocationFor(schedule)
		next, parseErr := parseScheduleTimeIn(schedule.NextRun, location)
		if parseErr != nil || next.IsZero() || next.After(time.Now().In(location)) {
			return Job{}, ErrInvalidState
		}
	}
	if operator == "" {
		operator = "scheduler"
	}
	if schedule.ConcurrencyPolicy == "forbid" || schedule.ConcurrencyPolicy == "replace" {
		active, activeErr := s.Get(schedule.LastJobID)
		if activeErr == nil && scheduleJobActive(active.Status) {
			if schedule.ConcurrencyPolicy == "forbid" {
				_ = s.recordScheduleResult(id, "skipped", advance)
				return Job{}, ErrScheduleConflict
			}
			if active.Status == "running" {
				if _, cancelErr := s.Cancel(active.ID); cancelErr != nil {
					_ = s.recordScheduleResult(id, "replace_failed", advance)
					return Job{}, ErrScheduleConflict
				}
			} else {
				_ = s.recordScheduleResult(id, "replace_failed", advance)
				return Job{}, ErrScheduleConflict
			}
		}
	}
	job, err := s.Create(CreateInput{TemplateID: schedule.TemplateID, PlaybookID: schedule.PlaybookID, Variables: cloneStringMap(schedule.Variables), Parameters: cloneStringMap(schedule.Parameters), Targets: schedule.Targets, Operator: operator, TimeoutSeconds: schedule.TimeoutSeconds, CredentialID: schedule.CredentialID, RetryMax: schedule.RetryMax, RetryIntervalSeconds: schedule.RetryIntervalSeconds})
	if err == nil && job.Status == "pending" {
		job, err = s.Run(job.ID)
	}
	status := "error"
	if job.ID != "" {
		status = job.Status
	}
	if recordErr := s.recordScheduleResult(id, status, advance); recordErr != nil && err == nil {
		err = recordErr
	}
	s.mu.Lock()
	for i := range s.schedules {
		if s.schedules[i].ID == id {
			s.schedules[i].LastJobID = job.ID
			if s.db != nil {
				_ = upsertSchedule(context.Background(), s.db, s.schedules[i])
			}
			break
		}
	}
	s.mu.Unlock()
	return job, err
}
func (s *Service) RunScheduler(ctx context.Context) {
	if s.db == nil {
		return
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		s.runDueSchedules(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) runDueSchedules(_ context.Context) {
	s.refreshSchedules()
	now := time.Now()
	type dueSchedule struct {
		ID     string
		Next   time.Time
		Missed string
	}
	s.mu.RLock()
	due := make([]dueSchedule, 0)
	type initialNextRun struct {
		ID   string
		Next string
	}
	initial := make([]initialNextRun, 0)
	for _, schedule := range s.schedules {
		if !schedule.Enabled {
			continue
		}
		location := scheduleLocationFor(schedule)
		if schedule.NextRun == "" {
			if next, err := nextCronTime(schedule.Cron, now.In(location)); err == nil {
				initial = append(initial, initialNextRun{ID: schedule.ID, Next: formatScheduleTimeIn(location, next)})
			}
			continue
		}
		next, err := parseScheduleTimeIn(schedule.NextRun, location)
		if err == nil && !next.After(now.In(location)) {
			due = append(due, dueSchedule{ID: schedule.ID, Next: next, Missed: schedule.MissedPolicy})
		}
	}
	s.mu.RUnlock()
	for _, item := range initial {
		_ = s.setScheduleNextRun(item.ID, item.Next)
	}
	for _, item := range due {
		if item.Missed == "skip" && now.Sub(item.Next) > time.Minute {
			_ = s.recordScheduleResult(item.ID, "missed_skipped", true)
			continue
		}
		_, _ = s.RunSchedule(item.ID, "scheduler", true)
	}
}

func cloneSchedule(schedule Schedule) Schedule {
	schedule.Targets = append([]string(nil), schedule.Targets...)
	schedule.Variables = cloneStringMap(schedule.Variables)
	schedule.Parameters = cloneStringMap(schedule.Parameters)
	schedule.NextRuns = append([]string(nil), schedule.NextRuns...)
	return schedule
}

func cloneSchedules(items []Schedule) []Schedule {
	out := make([]Schedule, 0, len(items))
	for _, item := range items {
		out = append(out, cloneSchedule(item))
	}
	return out
}

func formatScheduleTime(value time.Time) string {
	return formatScheduleTimeIn(scheduleLocation(), value)
}

func parseScheduleTime(value string) (time.Time, error) {
	return parseScheduleTimeIn(value, scheduleLocation())
}

func parseScheduleTimeIn(value string, location *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if location == nil {
		location = scheduleLocation()
	}
	return time.ParseInLocation(scheduleTimeLayout, value, location)
}

func validateCron(expression string) error {
	_, err := parseCron(expression)
	return err
}

func nextCronTime(expression string, from time.Time) (time.Time, error) {
	cron, err := parseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	location := from.Location()
	candidate := from.Truncate(time.Minute).Add(time.Minute)
	for dayOffset := 0; dayOffset < 366*5; dayOffset++ {
		day := candidate
		if dayOffset > 0 {
			day = candidate.AddDate(0, 0, dayOffset)
		}
		if !cron.month.contains(int(day.Month())) {
			continue
		}
		dayMatches := cron.day.contains(day.Day())
		weekMatches := cron.week.contains(int(day.Weekday()))
		if cron.day.wildcard || cron.week.wildcard {
			if !dayMatches || !weekMatches {
				continue
			}
		} else if !dayMatches && !weekMatches {
			continue
		}
		startHour, startMinute := 0, 0
		if dayOffset == 0 {
			startHour, startMinute = candidate.Hour(), candidate.Minute()
		}
		for _, hour := range cron.hour.values {
			if hour < startHour {
				continue
			}
			for _, minute := range cron.minute.values {
				if hour == startHour && minute < startMinute {
					continue
				}
				value := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, location)
				if value.After(from) {
					return value, nil
				}
			}
		}
	}
	return time.Time{}, fmt.Errorf("cron expression has no future run")
}
func parseCron(expression string) (cronSchedule, error) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(expression)))
	if len(fields) == 1 {
		aliases := map[string]string{
			"@hourly": "0 * * * *", "@daily": "0 0 * * *", "@midnight": "0 0 * * *",
			"@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *", "@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *",
		}
		value, ok := aliases[fields[0]]
		if !ok {
			return cronSchedule{}, fmt.Errorf("invalid cron alias")
		}
		fields = strings.Fields(value)
	}
	if len(fields) != 5 {
		return cronSchedule{}, fmt.Errorf("cron must contain five fields")
	}
	minute, err := parseCronField(fields[0], 0, 59, false)
	if err != nil {
		return cronSchedule{}, err
	}
	hour, err := parseCronField(fields[1], 0, 23, false)
	if err != nil {
		return cronSchedule{}, err
	}
	day, err := parseCronField(fields[2], 1, 31, false)
	if err != nil {
		return cronSchedule{}, err
	}
	month, err := parseCronField(fields[3], 1, 12, false)
	if err != nil {
		return cronSchedule{}, err
	}
	week, err := parseCronField(fields[4], 0, 7, true)
	if err != nil {
		return cronSchedule{}, err
	}
	return cronSchedule{minute: minute, hour: hour, day: day, month: month, week: week}, nil
}

func parseCronField(raw string, minimum, maximum int, normalizeSunday bool) (cronField, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cronField{}, fmt.Errorf("empty cron field")
	}
	result := cronField{wildcard: raw == "*"}
	values := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		step := 1
		valuePart := part
		if pieces := strings.Split(part, "/"); len(pieces) == 2 {
			valuePart = pieces[0]
			parsedStep, err := strconv.Atoi(pieces[1])
			if err != nil || parsedStep <= 0 {
				return cronField{}, fmt.Errorf("invalid cron step")
			}
			step = parsedStep
		} else if len(pieces) > 2 {
			return cronField{}, fmt.Errorf("invalid cron range")
		}
		start, end := 0, 0
		switch {
		case valuePart == "*":
			start, end = minimum, maximum
		case strings.Contains(valuePart, "-"):
			pieces := strings.Split(valuePart, "-")
			if len(pieces) != 2 {
				return cronField{}, fmt.Errorf("invalid cron range")
			}
			var err error
			start, err = strconv.Atoi(pieces[0])
			if err != nil {
				return cronField{}, err
			}
			end, err = strconv.Atoi(pieces[1])
			if err != nil {
				return cronField{}, err
			}
		default:
			value, err := strconv.Atoi(valuePart)
			if err != nil {
				return cronField{}, err
			}
			start, end = value, value
		}
		if start < minimum || end > maximum || start > end {
			return cronField{}, fmt.Errorf("cron value out of range")
		}
		for value := start; value <= end; value += step {
			normalized := value
			if normalizeSunday && normalized == 7 {
				normalized = 0
			}
			values[normalized] = true
		}
	}
	if len(values) == 0 {
		return cronField{}, fmt.Errorf("empty cron field")
	}
	for value := range values {
		result.values = append(result.values, value)
	}
	sort.Ints(result.values)
	return result, nil
}

func (field cronField) contains(value int) bool {
	for _, item := range field.values {
		if item == value {
			return true
		}
	}
	return false
}
