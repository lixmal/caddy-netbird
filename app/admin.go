package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

const pingTimeout = 5 * time.Second

func init() {
	caddy.RegisterModule(adminAPI{})
}

// adminAPI serves NetBird status and control endpoints on the Caddy admin API.
type adminAPI struct {
	app    *App
	logger *zap.Logger
}

// CaddyModule returns the Caddy module information.
func (adminAPI) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "admin.api.netbird",
		New: func() caddy.Module { return new(adminAPI) },
	}
}

// Provision obtains a reference to the netbird app.
func (a *adminAPI) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger()

	nbApp, err := ctx.AppIfConfigured("netbird")
	if err != nil {
		a.logger.Debug("netbird app not configured", zap.Error(err))
		return nil
	}
	a.app = nbApp.(*App)

	return nil
}

// Routes returns the admin routes for the NetBird endpoints.
func (a *adminAPI) Routes() []caddy.AdminRoute {
	return []caddy.AdminRoute{
		{
			Pattern: "/netbird/",
			Handler: caddy.AdminHandlerFunc(a.handleAPI),
		},
	}
}

// handleAPI routes requests to the appropriate handler.
func (a *adminAPI) handleAPI(w http.ResponseWriter, r *http.Request) error {
	if a.app == nil {
		http.Error(w, "netbird app not configured", http.StatusServiceUnavailable)
		return nil
	}

	path := strings.TrimPrefix(r.URL.Path, "/netbird/")
	switch {
	case path == "status" && r.Method == http.MethodGet:
		return a.handleStatus(w, r)
	case path == "log-level" && r.Method == http.MethodPut:
		return a.handleSetLogLevel(w, r)
	case path == "ping" && r.Method == http.MethodPost:
		return a.handlePing(w, r)
	default:
		return caddy.APIError{
			HTTPStatus: http.StatusNotFound,
			Err:        fmt.Errorf("unknown endpoint: %s", r.URL.Path),
		}
	}
}

type nodeName = string

type statusResponse struct {
	Nodes map[nodeName]*nodeStatus `json:"nodes"`
}

type nodeStatus struct {
	Local      localStatus      `json:"local"`
	Management managementStatus `json:"management"`
	Signal     signalStatus     `json:"signal"`
	Relays     []relayStatus    `json:"relays"`
	Peers      []peerStatus     `json:"peers"`
}

type localStatus struct {
	IP     string   `json:"ip"`
	FQDN   string   `json:"fqdn"`
	Routes []string `json:"routes,omitempty"`
}

type managementStatus struct {
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

type signalStatus struct {
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

type relayStatus struct {
	URI       string `json:"uri"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type peerStatus struct {
	IP            string        `json:"ip"`
	FQDN          string        `json:"fqdn"`
	ConnStatus    string        `json:"connStatus"`
	Relayed       bool          `json:"relayed"`
	Latency       time.Duration `json:"latency"`
	LastHandshake time.Time     `json:"lastHandshake"`
	BytesTx       int64         `json:"bytesTx"`
	BytesRx       int64         `json:"bytesRx"`
	Routes        []string      `json:"routes,omitempty"`
	RelayAddress  string        `json:"relayAddress,omitempty"`
	ICELocal      string        `json:"iceLocal,omitempty"`
	ICERemote     string        `json:"iceRemote,omitempty"`
}

// handleStatus returns the status of all NetBird nodes.
// Default output is human-readable text; use ?format=json for JSON.
func (a *adminAPI) handleStatus(w http.ResponseWriter, r *http.Request) error {
	resp := a.collectStatus()

	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(resp)
	}

	return a.writeStatusText(w, resp)
}

func (a *adminAPI) collectStatus() statusResponse {
	resp := statusResponse{
		Nodes: make(map[nodeName]*nodeStatus),
	}

	a.app.pool.Range(func(key, val any) bool {
		name := key.(string)
		mc := val.(*ManagedClient)

		fullStatus, err := mc.Client().Status()
		if err != nil {
			a.logger.Warn("get status", zap.String("node", name), zap.Error(err))
			return true
		}

		localRoutes := sortedKeys(fullStatus.LocalPeerState.Routes)

		ns := &nodeStatus{
			Local: localStatus{
				IP:     fullStatus.LocalPeerState.IP,
				FQDN:   fullStatus.LocalPeerState.FQDN,
				Routes: localRoutes,
			},
			Management: managementStatus{
				URL:       fullStatus.ManagementState.URL,
				Connected: fullStatus.ManagementState.Connected,
			},
			Signal: signalStatus{
				URL:       fullStatus.SignalState.URL,
				Connected: fullStatus.SignalState.Connected,
			},
		}

		if fullStatus.ManagementState.Error != nil {
			ns.Management.Error = fullStatus.ManagementState.Error.Error()
		}
		if fullStatus.SignalState.Error != nil {
			ns.Signal.Error = fullStatus.SignalState.Error.Error()
		}

		for _, r := range fullStatus.Relays {
			rs := relayStatus{
				URI:       r.URI,
				Available: r.Err == nil,
			}
			if r.Err != nil {
				rs.Error = r.Err.Error()
			}
			ns.Relays = append(ns.Relays, rs)
		}

		for _, p := range fullStatus.Peers {
			ns.Peers = append(ns.Peers, peerStatus{
				IP:            p.IP,
				FQDN:          p.FQDN,
				ConnStatus:    p.ConnStatus.String(),
				Relayed:       p.Relayed,
				Latency:       p.Latency,
				LastHandshake: p.LastWireguardHandshake,
				BytesTx:       p.BytesTx,
				BytesRx:       p.BytesRx,
				Routes:        sortedKeys(p.GetRoutes()),
				RelayAddress:  p.RelayServerAddress,
				ICELocal:      p.LocalIceCandidateEndpoint,
				ICERemote:     p.RemoteIceCandidateEndpoint,
			})
		}

		slices.SortFunc(ns.Peers, func(a, b peerStatus) int {
			return cmp.Compare(a.FQDN, b.FQDN)
		})

		resp.Nodes[name] = ns
		return true
	})

	return resp
}

// writeStatusText writes a human-readable status output similar to `netbird status`.
func (a *adminAPI) writeStatusText(w http.ResponseWriter, resp statusResponse) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	names := maps.Keys(resp.Nodes)
	slices.Sort(names)

	for _, name := range names {
		ns := resp.Nodes[name]
		fmt.Fprintf(tw, "Node: %s\n", name)
		fmt.Fprintf(tw, "  NetBird IP:\t%s\n", ns.Local.IP)
		fmt.Fprintf(tw, "  FQDN:\t%s\n", ns.Local.FQDN)
		if len(ns.Local.Routes) > 0 {
			fmt.Fprintf(tw, "  Routes:\t%s\n", strings.Join(ns.Local.Routes, ", "))
		}
		fmt.Fprintf(tw, "  Management:\t%s\t%s\n", ns.Management.URL, connectedStr(ns.Management.Connected))
		fmt.Fprintf(tw, "  Signal:\t%s\t%s\n", ns.Signal.URL, connectedStr(ns.Signal.Connected))

		for _, r := range ns.Relays {
			status := "Available"
			if !r.Available {
				status = "Unavailable"
			}
			fmt.Fprintf(tw, "  Relay:\t%s\t%s\n", r.URI, status)
		}

		fmt.Fprintln(tw)
		fmt.Fprintf(tw, "  Peers (%d):\n", len(ns.Peers))
		fmt.Fprintf(tw, "  FQDN\tIP\tStatus\tLatency\tTransfer\tConn\tHandshake\tRoutes\n")
		fmt.Fprintf(tw, "  ----\t--\t------\t-------\t--------\t----\t---------\t------\n")

		for _, p := range ns.Peers {
			connType := "P2P"
			if p.Relayed {
				connType = "Relayed"
			}
			if p.ConnStatus != "Connected" {
				connType = "-"
			}

			latency := "-"
			if p.Latency > 0 {
				latency = p.Latency.Round(time.Microsecond).String()
			}

			transfer := "-"
			if p.BytesTx > 0 || p.BytesRx > 0 {
				transfer = fmt.Sprintf("%s/%s", formatBytes(p.BytesRx), formatBytes(p.BytesTx))
			}

			handshake := "-"
			if !p.LastHandshake.IsZero() {
				handshake = time.Since(p.LastHandshake).Round(time.Second).String() + " ago"
			}

			routes := "-"
			if len(p.Routes) > 0 {
				routes = strings.Join(p.Routes, ", ")
			}

			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				p.FQDN, p.IP, p.ConnStatus, latency, transfer, connType, handshake, routes)
		}
		fmt.Fprintln(tw)
	}

	return tw.Flush()
}

func connectedStr(connected bool) string {
	if connected {
		return "Connected"
	}
	return "Disconnected"
}

func sortedKeys(m map[string]struct{}) []string {
	keys := maps.Keys(m)
	slices.Sort(keys)
	return keys
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

type logLevelRequest struct {
	Level string `json:"level"`
}

// handleSetLogLevel changes the NetBird log level at runtime.
func (a *adminAPI) handleSetLogLevel(w http.ResponseWriter, r *http.Request) error {
	var req logLevelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        fmt.Errorf("decode request: %w", err),
		}
	}

	var errs []error
	a.app.pool.Range(func(key, val any) bool {
		mc := val.(*ManagedClient)
		if err := mc.Client().SetLogLevel(req.Level); err != nil {
			errs = append(errs, fmt.Errorf("node %s: %w", key, err))
		}
		return true
	})
	if err := errors.Join(errs...); err != nil {
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        err,
		}
	}

	a.logger.Info("netbird log level changed", zap.String("level", req.Level))
	w.WriteHeader(http.StatusOK)
	return nil
}

type pingRequest struct {
	// Node is the name of the NetBird node to dial from.
	Node string `json:"node"`
	// Address is the target to reach via the NetBird network.
	// For tcp/udp: host:port. For ping (ICMP): just the host/IP.
	Address string `json:"address"`
	// Network is "tcp", "udp", or "ping" (ICMP). Default: "tcp".
	Network string `json:"network,omitempty"`
}

type pingResponse struct {
	Reachable bool          `json:"reachable"`
	Latency   time.Duration `json:"latency"`
	Error     string        `json:"error,omitempty"`
}

// handlePing performs a TCP, UDP, or ICMP ping through the NetBird network and measures RTT.
func (a *adminAPI) handlePing(w http.ResponseWriter, r *http.Request) error {
	var req pingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        fmt.Errorf("decode request: %w", err),
		}
	}

	if req.Address == "" {
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        fmt.Errorf("address is required"),
		}
	}
	if req.Node == "" {
		req.Node = "default"
	}
	if req.Network == "" {
		req.Network = "tcp"
	}

	switch req.Network {
	case "tcp", "udp", "ping":
	default:
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        fmt.Errorf("unsupported network %q: use tcp, udp, or ping", req.Network),
		}
	}

	mc, ok := a.app.LookupClient(req.Node)
	if !ok {
		return caddy.APIError{
			HTTPStatus: http.StatusNotFound,
			Err:        fmt.Errorf("node %q not found", req.Node),
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()

	var resp pingResponse
	if req.Network == "ping" {
		resp = a.doPingICMP(ctx, mc, req.Address)
	} else {
		resp = a.doPingDial(ctx, mc, req.Network, req.Address)
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(resp)
}

// doPingDial measures RTT via a TCP or UDP dial.
func (a *adminAPI) doPingDial(ctx context.Context, mc *ManagedClient, network, address string) pingResponse {
	start := time.Now()
	conn, err := mc.Client().DialContext(ctx, network, address)
	latency := time.Since(start)

	if err != nil {
		return pingResponse{Error: err.Error()}
	}
	conn.Close()

	return pingResponse{Reachable: true, Latency: latency}
}

// doPingICMP sends an ICMP echo request through the NetBird network using the "ping" network type.
func (a *adminAPI) doPingICMP(ctx context.Context, mc *ManagedClient, address string) pingResponse {
	conn, err := mc.Client().DialContext(ctx, "ping", address)
	if err != nil {
		return pingResponse{Error: err.Error()}
	}
	defer conn.Close()

	icmpReq := make([]byte, 8)
	icmpReq[0] = 8
	icmpReq[2], icmpReq[3] = icmpChecksum(icmpReq)

	start := time.Now()

	if _, err := conn.Write(icmpReq); err != nil {
		return pingResponse{Error: fmt.Sprintf("write echo request: %v", err)}
	}

	buf := make([]byte, 1500)
	if err := conn.SetReadDeadline(start.Add(pingTimeout)); err != nil {
		return pingResponse{Error: fmt.Sprintf("set read deadline: %v", err)}
	}
	if _, err := conn.Read(buf); err != nil {
		return pingResponse{Error: fmt.Sprintf("read echo reply: %v", err)}
	}

	latency := time.Since(start)
	return pingResponse{Reachable: true, Latency: latency}
}

// icmpChecksum computes the ICMP checksum per RFC 1071.
func icmpChecksum(data []byte) (byte, byte) {
	var sum uint32
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	csum := ^uint16(sum)
	return byte(csum >> 8), byte(csum)
}

var (
	_ caddy.Module      = (*adminAPI)(nil)
	_ caddy.AdminRouter = (*adminAPI)(nil)
	_ caddy.Provisioner = (*adminAPI)(nil)
)
