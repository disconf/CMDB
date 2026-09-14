package httpapi

import (
	"errors"
	"net/http"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/discovery"
)

func registerCollectionScheduleRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, discoveryService *discovery.Service) {
	mux.HandleFunc("GET /api/v1/discovery/schedules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		items, err := discoveryService.CollectionSchedules()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "SCHEDULE_LIST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("POST /api/v1/discovery/schedules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.CollectionScheduleInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SCHEDULE"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.CreateCollectionSchedule(input, user.Username)
		if err != nil {
			writeScheduleError(w, err)
			return
		}
		auditService.Record(user.Username, "discovery.schedule.create", item.ID, item.Name)
		writeJSON(w, http.StatusCreated, item)
	})
	mux.HandleFunc("PUT /api/v1/discovery/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.CollectionScheduleInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SCHEDULE"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.UpdateCollectionSchedule(r.PathValue("id"), input)
		if err != nil {
			writeScheduleError(w, err)
			return
		}
		auditService.Record(user.Username, "discovery.schedule.update", item.ID, item.Name)
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/schedules/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.ToggleCollectionSchedule(r.PathValue("id"))
		if err != nil {
			writeScheduleError(w, err)
			return
		}
		auditService.Record(user.Username, "discovery.schedule.toggle", item.ID, map[bool]string{true: "enabled", false: "disabled"}[item.Enabled])
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/schedules/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := discoveryService.RunCollectionSchedule(r.Context(), r.PathValue("id"), user.Username)
		if err != nil {
			writeScheduleError(w, err)
			return
		}
		auditService.Record(user.Username, "discovery.schedule.run", result.ScheduleID, "task="+result.TaskID+" status="+result.Status)
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("DELETE /api/v1/discovery/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if err := discoveryService.DeleteCollectionSchedule(r.PathValue("id")); err != nil {
			writeScheduleError(w, err)
			return
		}
		auditService.Record(user.Username, "discovery.schedule.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
}

func writeScheduleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, discovery.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "SCHEDULE_NOT_FOUND"})
	case errors.Is(err, discovery.ErrScheduleRunning):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "SCHEDULE_RUNNING", "message": err.Error()})
	default:
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "SCHEDULE_INVALID", "message": err.Error()})
	}
}
