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

func (s *Service) templateByID(id string) (Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.templates {
		if item.ID == id {
			return item, nil
		}
	}
	return Template{}, ErrNotFound
}

func normalizeScheduleInput(input ScheduleInput, template Template) (ScheduleInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.Cron = strings.TrimSpace(input.Cron)
	if input.Name == "" || input.TemplateID == "" || input.Cron == "" || len(input.Targets) == 0 || !template.Enabled {
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
	template, err := s.templateByID(input.TemplateID)
	if err != nil {
		return Schedule{}, ErrValidation
	}
	input, err = normalizeScheduleInput(input, template)
	if err != nil {
		return Schedule{}, err
	}
	next, err := nextCronTime(input.Cron, time.Now().In(s.location))
	if err != nil {
		return Schedule{}, ErrValidation
	}
	schedule := Schedule{ID: scheduleID(), Name: input.Name, TemplateID: template.ID, TemplateName: template.Name, Targets: append([]string(nil), input.Targets...), Cron: input.Cron, TimeoutSeconds: input.TimeoutSeconds, Enabled: *input.Enabled, NextRun: formatScheduleTime(next), CreatedBy: createdBy}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules = append([]Schedule{schedule}, s.schedules...)
	if s.db != nil {
		_ = upsertSchedule(context.Background(), s.db, schedule)
	}
	return cloneSchedule(schedule), nil
}
func (s *Service) UpdateSchedule(id string, input ScheduleInput) (Schedule, error) {
	template, err := s.templateByID(input.TemplateID)
	if err != nil {
		return Schedule{}, ErrValidation
	}
	input, err = normalizeScheduleInput(input, template)
	if err != nil {
		return Schedule{}, err
	}
	next, err := nextCronTime(input.Cron, time.Now().In(s.location))
	if err != nil {
		return Schedule{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID == id {
			s.schedules[i].Name = input.Name
			s.schedules[i].TemplateID = template.ID
			s.schedules[i].TemplateName = template.Name
			s.schedules[i].Targets = append([]string(nil), input.Targets...)
			s.schedules[i].Cron = input.Cron
			s.schedules[i].TimeoutSeconds = input.TimeoutSeconds
			s.schedules[i].Enabled = *input.Enabled
			if s.schedules[i].Enabled {
				s.schedules[i].NextRun = formatScheduleTime(next)
			} else {
				s.schedules[i].NextRun = ""
			}
			if s.db != nil {
				_ = upsertSchedule(context.Background(), s.db, s.schedules[i])
			}
			return cloneSchedule(s.schedules[i]), nil
		}
	}
	return Schedule{}, ErrNotFound
}

func (s *Service) ToggleSchedule(id string) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID == id {
			s.schedules[i].Enabled = !s.schedules[i].Enabled
			if s.schedules[i].Enabled {
				next, err := nextCronTime(s.schedules[i].Cron, time.Now().In(s.location))
				if err != nil {
					s.schedules[i].Enabled = false
					return Schedule{}, ErrValidation
				}
				s.schedules[i].NextRun = formatScheduleTime(next)
			} else {
				s.schedules[i].NextRun = ""
			}
			if s.db != nil {
				_ = upsertSchedule(context.Background(), s.db, s.schedules[i])
			}
			return cloneSchedule(s.schedules[i]), nil
		}
	}
	return Schedule{}, ErrNotFound
}

func (s *Service) DeleteSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID == id {
			s.schedules = append(s.schedules[:i], s.schedules[i+1:]...)
			if s.db != nil {
				return deleteScheduleRow(context.Background(), s.db, id)
			}
			return nil
		}
	}
	return ErrNotFound
}

func (s *Service) RunSchedule(id, operator string, advance bool) (Job, error) {
	s.mu.RLock()
	var schedule Schedule
	found := false
	for _, item := range s.schedules {
		if item.ID == id {
			schedule = cloneSchedule(item)
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return Job{}, ErrNotFound
	}
	if operator == "" {
		operator = "scheduler"
	}
	job, err := s.Create(CreateInput{TemplateID: schedule.TemplateID, Targets: schedule.Targets, Operator: operator, TimeoutSeconds: schedule.TimeoutSeconds})
	if err == nil && job.Status == "pending" {
		job, err = s.Run(job.ID)
	}
	status := "error"
	if job.ID != "" {
		status = job.Status
	}
	s.mu.Lock()
	for i := range s.schedules {
		if s.schedules[i].ID == id {
			s.schedules[i].LastRun = time.Now().In(s.location).Format(scheduleTimeLayout)
			s.schedules[i].LastStatus = status
			s.schedules[i].LastJobID = job.ID
			if advance {
				if next, nextErr := nextCronTime(s.schedules[i].Cron, time.Now().In(s.location)); nextErr == nil {
					s.schedules[i].NextRun = formatScheduleTime(next)
				}
			}
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
	now := time.Now().In(s.location)
	s.mu.RLock()
	due := make([]string, 0)
	for _, schedule := range s.schedules {
		if !schedule.Enabled {
			continue
		}
		if schedule.NextRun == "" {
			if next, err := nextCronTime(schedule.Cron, now); err == nil {
				schedule.NextRun = formatScheduleTime(next)
			}
			continue
		}
		next, err := time.ParseInLocation(scheduleTimeLayout, schedule.NextRun, s.location)
		if err == nil && !next.After(now) {
			due = append(due, schedule.ID)
		}
	}
	s.mu.RUnlock()
	for _, id := range due {
		_, _ = s.RunSchedule(id, "scheduler", true)
	}
}

func cloneSchedule(schedule Schedule) Schedule {
	schedule.Targets = append([]string(nil), schedule.Targets...)
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
	if value.IsZero() {
		return ""
	}
	return value.In(scheduleLocation()).Format(scheduleTimeLayout)
}

func parseScheduleTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation(scheduleTimeLayout, value, scheduleLocation())
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
