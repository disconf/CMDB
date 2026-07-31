package auth

type User struct {
	ID            string   `json:"id"`
	Username      string   `json:"username"`
	DisplayName   string   `json:"displayName"`
	Roles         []string `json:"roles"`
	Permissions   []string `json:"permissions"`
	ProjectGroups []string `json:"projectGroups"`
}

type Session struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type Module struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Icon       string `json:"icon"`
	Permission string `json:"permission"`
}
