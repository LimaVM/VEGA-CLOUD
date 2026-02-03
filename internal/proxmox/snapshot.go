package proxmox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// Snapshot representa um snapshot
type Snapshot struct {
	Name         string `json:"name"`
	SnapshotTime int64  `json:"snaptime"`
	Description  string `json:"description"`
	Parent       string `json:"parent"`
}

// ListSnapshots lista snapshots de um container
func (c *Client) ListSnapshots(ctid int) ([]Snapshot, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/snapshot", c.node, ctid)

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []Snapshot `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Filtra "current" que o Proxmox retorna como se fosse snapshot
	var snapshots []Snapshot
	for _, s := range result.Data {
		if s.Name != "current" {
			snapshots = append(snapshots, s)
		}
	}

	return snapshots, nil
}

// CreateSnapshot cria um snapshot
func (c *Client) CreateSnapshot(ctid int, name, description string) error {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/snapshot", c.node, ctid)

	data := url.Values{}
	data.Set("snapname", name)
	data.Set("description", description)

	resp, err := c.doRequest("POST", path, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create snapshot failed: %s", string(body))
	}

	// Wait for snapshot to complete (simple wait, ideal would be task check)
	time.Sleep(2 * time.Second)

	return nil
}

// RollbackSnapshot restaura um snapshot
func (c *Client) RollbackSnapshot(ctid int, name string) error {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/snapshot/%s/rollback", c.node, ctid, name)

	resp, err := c.doRequest("POST", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rollback snapshot failed: %s", string(body))
	}

	// Wait for rollback
	time.Sleep(5 * time.Second)

	return nil
}

// DeleteSnapshot deleta um snapshot
func (c *Client) DeleteSnapshot(ctid int, name string) error {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/snapshot/%s", c.node, ctid, name)

	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete snapshot failed: %s", string(body))
	}

	return nil
}
