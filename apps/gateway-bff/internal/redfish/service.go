package redfish

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct{}
type System struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	SerialNumber string `json:"serialNumber"`
	PowerState   string `json:"powerState"`
}
type Result struct {
	Address string   `json:"address"`
	Systems []System `json:"systems"`
}

func (Client) Test(ctx context.Context, address, username, password string) (Result, error) {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	if address == "" {
		return Result{}, errors.New("address is required")
	}
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "https://" + address
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/redfish/v1/Systems", nil)
	if err != nil {
		return Result{}, err
	}
	if username != "" {
		request.SetBasicAuth(username, password)
	}
	client := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	response, err := client.Do(request)
	if err != nil {
		return Result{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("Redfish returned %s", response.Status)
	}
	var listing struct {
		Members []struct {
			ID string `json:"@odata.id"`
		} `json:"Members"`
	}
	if err = json.NewDecoder(response.Body).Decode(&listing); err != nil {
		return Result{}, err
	}
	result := Result{Address: address, Systems: []System{}}
	for _, member := range listing.Members {
		detail, err := http.NewRequestWithContext(ctx, http.MethodGet, address+member.ID, nil)
		if err != nil {
			continue
		}
		if username != "" {
			detail.SetBasicAuth(username, password)
		}
		response, err := client.Do(detail)
		if err != nil {
			continue
		}
		var system System
		_ = json.NewDecoder(response.Body).Decode(&system)
		response.Body.Close()
		if system.ID == "" {
			system.ID = strings.TrimPrefix(member.ID, "/redfish/v1/Systems/")
		}
		result.Systems = append(result.Systems, system)
	}
	return result, nil
}
