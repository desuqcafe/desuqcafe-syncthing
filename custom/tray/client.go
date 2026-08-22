package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// endpoint is where the local Syncthing's REST API lives, plus the key needed
// to talk to it. Both come out of config.xml, which is the only place they are
// written down: Syncthing probes for a free GUI port on first start, so the
// address cannot be assumed to be 8384 and has to be re-read rather than
// remembered.
type endpoint struct {
	baseURL string
	apiKey  string
}

// configXML is the sliver of Syncthing's config we care about. Decoding into a
// narrow struct rather than the real config.Configuration is what lets this be
// a separate module: it means not importing lib/config, and not caring when
// upstream adds a field.
type configXML struct {
	GUI struct {
		Address string `xml:"address"`
		APIKey  string `xml:"apikey"`
		TLS     string `xml:"tls,attr"`
	} `xml:"gui"`
}

func readEndpoint(home string) (endpoint, error) {
	raw, err := os.ReadFile(filepath.Join(home, "config.xml"))
	if err != nil {
		return endpoint{}, err
	}

	var cfg configXML
	if err := xml.Unmarshal(raw, &cfg); err != nil {
		return endpoint{}, fmt.Errorf("parse config.xml: %w", err)
	}
	if cfg.GUI.Address == "" {
		return endpoint{}, fmt.Errorf("config.xml has no GUI address")
	}

	scheme := "http"
	if cfg.GUI.TLS == "true" {
		scheme = "https"
	}

	addr := cfg.GUI.Address
	// A GUI bound to all interfaces still has to be reached by a real host.
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host == "" || host == "0.0.0.0" || host == "::" {
			addr = net.JoinHostPort("127.0.0.1", port)
		}
	}

	return endpoint{
		baseURL: scheme + "://" + addr,
		apiKey:  cfg.GUI.APIKey,
	}, nil
}

type client struct {
	ep   endpoint
	http *http.Client
}

func newClient(ep endpoint) *client {
	return &client{
		ep: ep,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				// The GUI certificate is self-signed and regenerated per
				// install; there is nothing to pin it against. This only ever
				// talks to loopback, so there is no meaningful attacker in the
				// path to protect from either.
				TLSClientConfig: insecureLoopbackTLS(),
			},
		},
	}
}

func (c *client) do(method, path string, query url.Values, body, out any) error {
	u := c.ep.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader *strings.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(encoded))
	}

	var req *http.Request
	var err error
	if reader != nil {
		req, err = http.NewRequest(method, u, reader)
	} else {
		req, err = http.NewRequest(method, u, nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.ep.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) get(path string, query url.Values, out any) error {
	return c.do(http.MethodGet, path, query, nil, out)
}

func (c *client) post(path string, query url.Values) error {
	return c.do(http.MethodPost, path, query, nil, nil)
}

// --- the handful of API shapes we read -----------------------------------

type restConfig struct {
	Devices []struct {
		DeviceID string `json:"deviceID"`
		Name     string `json:"name"`
		Paused   bool   `json:"paused"`
	} `json:"devices"`
	Folders []struct {
		ID     string `json:"id"`
		Label  string `json:"label"`
		Paused bool   `json:"paused"`
	} `json:"folders"`
}

type folderStatus struct {
	State     string `json:"state"`
	NeedBytes int64  `json:"needBytes"`
	NeedItems int64  `json:"needItems"`
	Errors    int    `json:"errors"`
}

type connections struct {
	Connections map[string]struct {
		Connected bool `json:"connected"`
	} `json:"connections"`
}

func (c *client) config() (restConfig, error) {
	var out restConfig
	err := c.get("/rest/config", nil, &out)
	return out, err
}

func (c *client) folderStatus(id string) (folderStatus, error) {
	var out folderStatus
	err := c.get("/rest/db/status", url.Values{"folder": {id}}, &out)
	return out, err
}

func (c *client) connections() (connections, error) {
	var out connections
	err := c.get("/rest/system/connections", nil, &out)
	return out, err
}

// pauseAll and resumeAll hit the same endpoints as the GUI's "Pause All"
// button: with no device parameter, Syncthing sets Paused on every configured
// device, which stops all network sync while leaving local scanning alone.
func (c *client) pauseAll() error  { return c.post("/rest/system/pause", nil) }
func (c *client) resumeAll() error { return c.post("/rest/system/resume", nil) }

func (c *client) shutdown() error { return c.post("/rest/system/shutdown", nil) }

func (c *client) ping() error { return c.get("/rest/system/ping", nil, nil) }
