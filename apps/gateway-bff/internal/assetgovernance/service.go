package assetgovernance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

var ErrNotFound = errors.New("asset governance issue not found")
var ErrValidation = errors.New("asset governance validation failed")

type Issue struct {
	AssetID       string `json:"assetId"`
	AssetName     string `json:"assetName"`
	AssetType     string `json:"assetType"`
	AssetTypeName string `json:"assetTypeName"`
	IP            string `json:"ip"`
	Status        string `json:"status"`
	ProjectGroup  string `json:"projectGroup"`
	Owner         string `json:"owner"`
	Location      string `json:"location"`
	IssueType     string `json:"issueType"`
	Severity      string `json:"severity"`
	Detail        string `json:"detail"`
	LastSeenAt    string `json:"lastSeenAt"`
	StaleDays     int    `json:"staleDays"`
	ReviewStatus  string `json:"reviewStatus"`
	ReviewedAt    string `json:"reviewedAt,omitempty"`
	ReviewedBy    string `json:"reviewedBy,omitempty"`
	Note          string `json:"note,omitempty"`
	TicketID      string `json:"ticketId,omitempty"`
	UpdatedAt     string `json:"updatedAt"`
}

type Review struct {
	AssetID      string `json:"assetId"`
	Status       string `json:"status"`
	Owner        string `json:"owner"`
	ProjectGroup string `json:"projectGroup"`
	Location     string `json:"location"`
	ReviewedAt   string `json:"reviewedAt,omitempty"`
	ReviewedBy   string `json:"reviewedBy,omitempty"`
	Note         string `json:"note,omitempty"`
	TicketID     string `json:"ticketId,omitempty"`
	UpdatedAt    string `json:"updatedAt"`
}

type Service struct {
	mu      sync.RWMutex
	db      *sql.DB
	cmdb    *cmdb.Service
	tickets *tickets.Service
	monitor *monitor.Service
	reviews map[string]Review
}

func NewService(cmdbService *cmdb.Service, ticketsService *tickets.Service, monitorService *monitor.Service) *Service {
	service := &Service{cmdb: cmdbService, tickets: ticketsService, monitor: monitorService, reviews: map[string]Review{}}
	if url := strings.TrimSpace(os.Getenv("DATABASE_URL")); url != "" {
		db, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize asset governance repository: %v", err))
		}
		service.db = db
		reviews, loadErr := loadReviews(context.Background(), db)
		if loadErr != nil {
			panic(fmt.Sprintf("load asset governance reviews: %v", loadErr))
		}
		service.reviews = reviews
	}
	return service
}

func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.Evaluate("asset-governance")
		}
	}
}
