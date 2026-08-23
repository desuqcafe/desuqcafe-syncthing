package main

import (
	"context"
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
	// Folders are read from the file rather than the API only by
	// --clear-folder-icons, which has to work with Syncthing stopped: that is
	// the state the uninstaller leaves things in, and the state anyone
	// reaching for the flag is most likely to be in.
	Folders []struct {
		ID    string `xml:"id,attr"`
		Path  string `xml:"path,attr"`
		Label string `xml:"label,attr"`
	} `xml:"folder"`
}

// folderPathsFromConfig reads the configured folder paths straight off disk.
func folderPathsFromConfig(home string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(home, "config.xml"))
	if err != nil {
		return nil, err
	}
	var cfg configXML
	if err := xml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config.xml: %w", err)
	}
	paths := make([]string, 0, len(cfg.Folders))
	for _, f := range cfg.Folders {
		if f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	return paths, nil
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
	// long is for /rest/events, which deliberately blocks. It cannot share the
	// ten-second client: a long poll that is working looks exactly like a
	// request that has hung, so it needs a client with no deadline of its own
	// and a context to bound it instead.
	long *http.Client
}

func newClient(ep endpoint) *client {
	transport := func() *http.Transport {
		return &http.Transport{
			// The GUI certificate is self-signed and regenerated per install;
			// there is nothing to pin it against. This only ever talks to
			// loopback, so there is no meaningful attacker in the path to
			// protect from either.
			TLSClientConfig: insecureLoopbackTLS(),
		}
	}
	return &client{
		ep: ep,
		http: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport(),
		},
		long: &http.Client{
			// A margin over the server's own timeout, so a healthy poll always
			// returns on the server's schedule rather than being cut off here.
			Timeout:   eventPollTimeout + 30*time.Second,
			Transport: transport(),
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

// getLongPoll is get for endpoints that block on purpose. The context, not a
// client timeout, is what ends the request.
func (c *client) getLongPoll(ctx context.Context, path string, query url.Values, out any) error {
	u := c.ep.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.ep.apiKey)

	resp, err := c.long.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) post(path string, query url.Values) error {
	return c.do(http.MethodPost, path, query, nil, nil)
}

// --- the handful of API shapes we read -----------------------------------

type restConfig struct {
	Devices []restDevice `json:"devices"`
	Folders []restFolder `json:"folders"`
}

type restDevice struct {
	DeviceID string `json:"deviceID"`
	Name     string `json:"name"`
	Paused   bool   `json:"paused"`
}

type restFolder struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Paused bool   `json:"paused"`
	// MinDiskFree is the reserve Syncthing refuses to pull below. The unit is
	// one of "%", "kB", "MB", "GB", "TB"; an empty unit means bytes.
	MinDiskFree struct {
		Value float64 `json:"value"`
		Unit  string  `json:"unit"`
	} `json:"minDiskFree"`
}

// name is the label if there is one, falling back to the folder ID, which is
// what the GUI shows too.
func (f restFolder) name() string {
	if f.Label != "" {
		return f.Label
	}
	return f.ID
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
		// ClientVersion is what the remote said it was running in its Hello
		// message. Upstream has always served it -- the GUI shows it in the
		// device detail table -- and it is the whole basis of update.go's
		// "somebody you sync with is ahead of you" check. Empty for a device
		// that has never connected.
		ClientVersion string `json:"clientVersion"`
	} `json:"connections"`
}

// systemVersion is /rest/system/version. Only Version is read: it is this
// build's own stamp, the other side of update.go's comparison.
type systemVersion struct {
	Version string `json:"version"`
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

func (c *client) version() (string, error) {
	var out systemVersion
	err := c.get("/rest/system/version", nil, &out)
	return out.Version, err
}

// pauseAll and resumeAll hit the same endpoints as the GUI's "Pause All"
// button: with no device parameter, Syncthing sets Paused on every configured
// device, which stops all network sync while leaving local scanning alone.
func (c *client) pauseAll() error  { return c.post("/rest/system/pause", nil) }
func (c *client) resumeAll() error { return c.post("/rest/system/resume", nil) }

func (c *client) shutdown() error { return c.post("/rest/system/shutdown", nil) }

// --- what the notifications need -----------------------------------------

// pendingDevice is one entry of /rest/cluster/pending/devices, keyed by device
// ID: a device that has tried to connect and is not in the configuration.
type pendingDevice struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// pendingFolder is one entry of /rest/cluster/pending/folders, keyed by folder
// ID. offeredBy is keyed by the device ID doing the offering.
type pendingFolder struct {
	OfferedBy map[string]struct {
		Label string `json:"label"`
		Time  string `json:"time"`
	} `json:"offeredBy"`
}

func (c *client) pendingDevices() (map[string]pendingDevice, error) {
	var out map[string]pendingDevice
	err := c.get("/rest/cluster/pending/devices", nil, &out)
	return out, err
}

func (c *client) pendingFolders() (map[string]pendingFolder, error) {
	var out map[string]pendingFolder
	err := c.get("/rest/cluster/pending/folders", nil, &out)
	return out, err
}

// diskFree is the fork's own endpoint -- upstream reports no disk usage at all
// -- so the tray is talking to a build that has it. An older or stock
// Syncthing simply 404s and the disk warnings stay off.
type diskFree struct {
	Path         string `json:"path"`
	MeasuredPath string `json:"measuredPath"`
	Free         uint64 `json:"free"`
	Total        uint64 `json:"total"`
}

func (c *client) diskFree(path string) (diskFree, error) {
	var out diskFree
	err := c.get("/rest/system/diskfree", url.Values{"path": {path}}, &out)
	return out, err
}

func (c *client) ping() error { return c.get("/rest/system/ping", nil, nil) }

// --- ignore patterns, for the Explorer folder marker ----------------------

// folderIgnores is the shape of /rest/db/ignores. Error carries a *parse*
// failure, which arrives with a 200: Syncthing stored the lines and then found
// it could not read them back. A folder in that state refuses to scan or pull
// at all, so the field has to be looked at rather than assumed empty.
type folderIgnores struct {
	Ignore   []string `json:"ignore"`
	Expanded []string `json:"expanded"`
	Error    string   `json:"error"`
}

func (c *client) ignores(folderID string) (folderIgnores, error) {
	var out folderIgnores
	err := c.get("/rest/db/ignores", url.Values{"folder": {folderID}}, &out)
	return out, err
}

func (c *client) setIgnores(folderID string, lines []string) error {
	var out folderIgnores
	body := map[string][]string{"ignore": lines}
	err := c.do(http.MethodPost, "/rest/db/ignores",
		url.Values{"folder": {folderID}}, body, &out)
	if err != nil {
		return err
	}
	if out.Error != "" {
		return fmt.Errorf("ignore patterns: %s", out.Error)
	}
	return nil
}
