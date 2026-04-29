package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"
)

var config struct {
	Address   string
	LogLevel  string
	LogFormat string
}

// statePayload matches the JSON structure Envoy Gateway encodes in the state parameter.
type statePayload struct {
	URL       string `json:"url"`
	CSRFToken string `json:"csrf_token"`
	FlowID    string `json:"flow_id"`
}

// extractOriginalURL decodes the OIDC state parameter.
// Envoy Gateway encodes it as base64url(JSON{url, csrf_token, flow_id}).
func extractOriginalURL(state string) (*string, error) {
	// URL-decode in case the state was percent-encoded
	decoded, err := url.QueryUnescape(state)
	if err != nil {
		decoded = state
	}
	decorders := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.StdEncoding,
	}
	err = nil
	for _, decoder := range decorders {
		raw, _err := decoder.DecodeString(decoded)
		if _err != nil {
			err = errors.Join(err, _err)
			continue
		}

		var payload statePayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}

		if payload.URL == "" {
			return nil, errors.New("state payload contains no url field")
		}
		return &payload.URL, nil
	}
	return nil, err
}

func callbackHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		slog.Error("no state parameter")
		http.NotFound(w, r)
		return
	}

	target, err := extractOriginalURL(state)
	if err != nil {
		slog.Error("failed to decode state", "state", state, "errror", err)
		http.NotFound(w, r)
		return
	}

	slog.Debug("redirection", "state", state, "url", *target)
	http.Redirect(w, r, *target, http.StatusFound)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

var rootCmd = &cobra.Command{
	Use: "oidc-redirect",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		var lvl slog.Level
		var logger *slog.Logger
		if err := lvl.UnmarshalText([]byte(config.LogLevel)); err != nil {
			return err
		}
		switch config.LogFormat {
		case "fmt":
			logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
		case "json":
			logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
		default:
			return errors.New("log-format invalid")
		}
		slog.SetDefault(logger)
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		mux := http.NewServeMux()
		mux.HandleFunc("/oauth/redirect", callbackHandler)
		mux.HandleFunc("/healthz", healthHandler)

		slog.Info("listening", "addr", config.Address)
		if err := http.ListenAndServe(config.Address, mux); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	flags := rootCmd.Flags()
	flags.StringVar(&config.Address, "address", ":8080", "Address for listening")
	flags.StringVar(&config.LogLevel, "log-level", "INFO", "loglevel")
	flags.StringVar(&config.LogFormat, "log-format", "json", "logformat (fmt, json)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		slog.Error("startup", "error", err)
	}
}
