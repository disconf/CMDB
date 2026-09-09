package idc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrNotFound = errors.New("idc: not found")
var ErrNoDB = errors.New("idc: database not configured")

type Service struct {
	db *sql.DB
}

func NewService() *Service {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return &Service{}
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return &Service{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = db.PingContext(ctx)
	_, _ = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS idc_rooms (
		id text PRIMARY KEY, name text NOT NULL, location text NOT NULL DEFAULT '',
		created_at timestamptz NOT NULL DEFAULT now());
	CREATE TABLE IF NOT EXISTS idc_modules (
		id text PRIMARY KEY, room_id text NOT NULL REFERENCES idc_rooms(id) ON DELETE CASCADE,
		name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
	CREATE TABLE IF NOT EXISTS idc_racks (
		id text PRIMARY KEY, module_id text NOT NULL REFERENCES idc_modules(id) ON DELETE CASCADE,
		name text NOT NULL, u_total integer NOT NULL DEFAULT 42,
		voltage text NOT NULL DEFAULT '220V', created_at timestamptz NOT NULL DEFAULT now());
	CREATE TABLE IF NOT EXISTS idc_rack_occupancy (
		rack_id text NOT NULL REFERENCES idc_racks(id),
		u_position text NOT NULL,
		asset_id text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (rack_id, u_position));`)
	return &Service{db: db}
}

func newID(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }

func (s *Service) ListRooms() ([]Room, error) {
	if s.db == nil {
		return []Room{}, ErrNoDB
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,location FROM idc_rooms ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rooms := []Room{}
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Name, &r.Location); err != nil {
			return nil, err
		}
		r.Modules = []Module{}
		rooms = append(rooms, r)
	}
	_ = rows.Close()
	for i := range rooms {
		modRows, err := s.db.QueryContext(ctx, `SELECT id,room_id,name FROM idc_modules WHERE room_id=$1 ORDER BY created_at`, rooms[i].ID)
		if err != nil {
			return nil, err
		}
		for modRows.Next() {
			var m Module
			if err := modRows.Scan(&m.ID, &m.RoomID, &m.Name); err == nil {
				m.Racks = []Rack{}
				rooms[i].Modules = append(rooms[i].Modules, m)
			}
		}
		modRows.Close()
		for j := range rooms[i].Modules {
			rackRows, err := s.db.QueryContext(ctx, `SELECT id,module_id,name,u_total,voltage FROM idc_racks WHERE module_id=$1 ORDER BY created_at`, rooms[i].Modules[j].ID)
			if err != nil {
				return nil, err
			}
			for rackRows.Next() {
				var k Rack
				if err := rackRows.Scan(&k.ID, &k.ModuleID, &k.Name, &k.UTotal, &k.Voltage); err == nil {
					rooms[i].Modules[j].Racks = append(rooms[i].Modules[j].Racks, k)
				}
			}
			rackRows.Close()
			if n := len(rooms[i].Modules[j].Racks); n > 0 {
				occupied, err := s.occupiedU(ctx, rooms[i].Modules[j].Racks[n-1].ID)
				if err != nil {
					return nil, err
				}
				rooms[i].Modules[j].Racks[n-1].OccupiedU = occupied
			}
		}
	}
	return rooms, nil
}

func (s *Service) occupiedU(ctx context.Context, rackID string) ([]string, error) {
	filter, _ := json.Marshal([]map[string]string{{"name": "rack_id", "value": rackID}})
	rows, err := s.db.QueryContext(ctx, `SELECT attributes FROM cmdb_assets WHERE attributes @> $1::jsonb`, string(filter))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var occupied []string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var attrs []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if json.Unmarshal(raw, &attrs) == nil {
			for _, a := range attrs {
				if a.Name == "u_position" && a.Value != "" {
					occupied = append(occupied, a.Value)
				}
			}
		}
	}
	return occupied, rows.Err()
}

var ErrOccupied = errors.New("idc: rack has occupied U positions")

func (s *Service) rackOccupied(rackID string) (bool, error) {
	list, err := s.occupiedU(context.Background(), rackID)
	if err != nil {
		return false, err
	}
	return len(list) > 0, nil
}

func (s *Service) UpdateRack(id string, in UpdateRack) (Rack, error) {
	if s.db == nil {
		return Rack{}, ErrNoDB
	}
	if strings.TrimSpace(in.Name) == "" {
		return Rack{}, errors.New("idc: name required")
	}
	u := in.UTotal
	if u <= 0 {
		u = 42
	}
	v := in.Voltage
	if v == "" {
		v = "220V"
	}
	res, err := s.db.Exec(`UPDATE idc_racks SET name=$2,u_total=$3,voltage=$4 WHERE id=$1`, id, in.Name, u, v)
	if err != nil {
		return Rack{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Rack{}, ErrNotFound
	}
	return Rack{ID: id, Name: in.Name, UTotal: u, Voltage: v}, nil
}

func (s *Service) UpdateModule(id string, in UpdateModule) (Module, error) {
	if s.db == nil {
		return Module{}, ErrNoDB
	}
	if strings.TrimSpace(in.Name) == "" {
		return Module{}, errors.New("idc: name required")
	}
	res, err := s.db.Exec(`UPDATE idc_modules SET name=$2 WHERE id=$1`, id, in.Name)
	if err != nil {
		return Module{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Module{}, ErrNotFound
	}
	return Module{ID: id, Name: in.Name}, nil
}

func (s *Service) DeleteRack(id string) error {
	if s.db == nil {
		return ErrNoDB
	}
	busy, err := s.rackOccupied(id)
	if err != nil {
		return err
	}
	if busy {
		return ErrOccupied
	}
	res, err := s.db.Exec(`DELETE FROM idc_racks WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeleteModule(id string) error {
	if s.db == nil {
		return ErrNoDB
	}
	res, err := s.db.Exec(`DELETE FROM idc_modules WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeleteRoom(id string) error {
	if s.db == nil {
		return ErrNoDB
	}
	res, err := s.db.Exec(`DELETE FROM idc_rooms WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) CreateRoom(in CreateRoom) (Room, error) {
	if s.db == nil {
		return Room{}, ErrNoDB
	}
	if strings.TrimSpace(in.Name) == "" {
		return Room{}, errors.New("idc: name required")
	}
	room := Room{ID: newID("room"), Name: in.Name, Location: in.Location, Modules: []Module{}}
	_, err := s.db.Exec(`INSERT INTO idc_rooms(id,name,location) VALUES($1,$2,$3)`, room.ID, room.Name, room.Location)
	if err != nil {
		return Room{}, err
	}
	return room, nil
}

func (s *Service) AddModule(in CreateModule) (Module, error) {
	if s.db == nil {
		return Module{}, ErrNoDB
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.RoomID) == "" {
		return Module{}, errors.New("idc: roomId and name required")
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM idc_rooms WHERE id=$1`, in.RoomID).Scan(&exists); err != nil || exists == 0 {
		return Module{}, ErrNotFound
	}
	m := Module{ID: newID("mod"), RoomID: in.RoomID, Name: in.Name, Racks: []Rack{}}
	if _, err := s.db.Exec(`INSERT INTO idc_modules(id,room_id,name) VALUES($1,$2,$3)`, m.ID, m.RoomID, m.Name); err != nil {
		return Module{}, err
	}
	return m, nil
}

func (s *Service) AddRack(in CreateRack) (Rack, error) {
	if s.db == nil {
		return Rack{}, ErrNoDB
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.ModuleID) == "" {
		return Rack{}, errors.New("idc: moduleId and name required")
	}
	if in.UTotal <= 0 {
		in.UTotal = 42
	}
	if in.Voltage == "" {
		in.Voltage = "220V"
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM idc_modules WHERE id=$1`, in.ModuleID).Scan(&exists); err != nil || exists == 0 {
		return Rack{}, ErrNotFound
	}
	k := Rack{ID: newID("rack"), ModuleID: in.ModuleID, Name: in.Name, UTotal: in.UTotal, Voltage: in.Voltage}
	if _, err := s.db.Exec(`INSERT INTO idc_racks(id,module_id,name,u_total,voltage) VALUES($1,$2,$3,$4,$5)`, k.ID, k.ModuleID, k.Name, k.UTotal, k.Voltage); err != nil {
		return Rack{}, err
	}
	return k, nil
}
