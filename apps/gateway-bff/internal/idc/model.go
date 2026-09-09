package idc

// Room is a data-center room with modules and racks.
type Room struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Location string   `json:"location"`
	Modules  []Module `json:"modules"`
}
type Module struct {
	ID     string `json:"id"`
	RoomID string `json:"roomId"`
	Name   string `json:"name"`
	Racks  []Rack `json:"racks"`
}
type Rack struct {
	ID        string   `json:"id"`
	ModuleID  string   `json:"moduleId"`
	Name      string   `json:"name"`
	UTotal    int      `json:"uTotal"`
	Voltage   string   `json:"voltage"`
	OccupiedU []string `json:"occupiedU"`
}
type CreateRoom struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}
type CreateModule struct {
	RoomID string `json:"roomId"`
	Name   string `json:"name"`
}
type CreateRack struct {
	ModuleID string `json:"moduleId"`
	Name     string `json:"name"`
	UTotal   int    `json:"uTotal"`
	Voltage  string `json:"voltage"`
}

type UpdateRack struct {
	Name    string `json:"name"`
	UTotal  int    `json:"uTotal"`
	Voltage string `json:"voltage"`
}
type UpdateModule struct {
	Name string `json:"name"`
}
