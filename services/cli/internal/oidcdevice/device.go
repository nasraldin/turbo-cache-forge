package oidcdevice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrAccessDenied = errors.New("oidcdevice: user denied access")
var ErrExpired = errors.New("oidcdevice: device code expired before authorization")

type Discovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

// Discover fetches the issuer's OIDC discovery document. Any OIDC provider
// that supports device flow (Clerk, Keycloak, ZITADEL, Auth.js) serves one —
// this keeps the CLI provider-agnostic, matching the backend's own stance.
func Discover(ctx context.Context, issuer string) (Discovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return Discovery{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Discovery{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Discovery{}, fmt.Errorf("discovery %s: status %d", issuer, resp.StatusCode)
	}
	var d Discovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return Discovery{}, err
	}
	if d.DeviceAuthorizationEndpoint == "" || d.TokenEndpoint == "" {
		return Discovery{}, fmt.Errorf("issuer %s does not advertise a device_authorization_endpoint", issuer)
	}
	return d, nil
}

type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// RequestDeviceCode starts RFC 8628 device authorization.
func RequestDeviceCode(ctx context.Context, disc Discovery, clientID, scope string) (DeviceAuthResponse, error) {
	form := url.Values{"client_id": {clientID}, "scope": {scope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.DeviceAuthorizationEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return DeviceAuthResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return DeviceAuthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DeviceAuthResponse{}, fmt.Errorf("device_authorization: status %d", resp.StatusCode)
	}
	var d DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return DeviceAuthResponse{}, err
	}
	return d, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

// PollToken implements the RFC 8628 polling loop. sleep is injected so tests
// never wait in real time; production callers pass time.Sleep.
func PollToken(ctx context.Context, disc Discovery, clientID, deviceCode string, interval time.Duration, deadline time.Time, sleep func(time.Duration)) (string, error) {
	for {
		if time.Now().After(deadline) {
			return "", ErrExpired
		}
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {clientID},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint,
			strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		var tr tokenResponse
		decErr := json.NewDecoder(resp.Body).Decode(&tr)
		resp.Body.Close()
		if decErr != nil {
			return "", decErr
		}
		switch tr.Error {
		case "":
			if tr.AccessToken != "" {
				return tr.AccessToken, nil
			}
		case "authorization_pending":
			// expected — the user hasn't approved yet
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return "", ErrAccessDenied
		case "expired_token":
			return "", ErrExpired
		default:
			return "", fmt.Errorf("oidcdevice: token error %q", tr.Error)
		}
		sleep(interval)
	}
}

// Login runs the full device flow: discover the issuer's endpoints, request
// a device code, hand the user the code+URL via prompt, then poll for the
// token.
func Login(ctx context.Context, issuer, clientID, scope string, prompt func(DeviceAuthResponse), sleep func(time.Duration)) (string, error) {
	disc, err := Discover(ctx, issuer)
	if err != nil {
		return "", err
	}
	da, err := RequestDeviceCode(ctx, disc, clientID, scope)
	if err != nil {
		return "", err
	}
	prompt(da)
	interval := time.Duration(da.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(da.ExpiresIn) * time.Second)
	return PollToken(ctx, disc, clientID, da.DeviceCode, interval, deadline, sleep)
}
