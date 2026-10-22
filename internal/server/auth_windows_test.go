//go:build windows

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"macbrowser/internal/config"
)

func TestAuthTokenRequired(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(cfg, nil, "")
	s.SetAuthToken("test token")
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/roots")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated code = %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/roots", nil)
	req.Header.Set("Authorization", "Bearer test token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer code = %d", resp.StatusCode)
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = noRedirect.Get(ts.URL + "/?token=" + url.QueryEscape("test token"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login code = %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("login location = %q", loc)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == cookieNameForToken("test token") {
			session = c
		}
	}
	if session == nil {
		t.Fatal("missing auth cookie")
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" {
		t.Fatalf("cookie flags = %+v", session)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/roots", nil)
	req.AddCookie(session)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie code = %d", resp.StatusCode)
	}

	resp, err = noRedirect.Get(ts.URL + "/?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token code = %d", resp.StatusCode)
	}
}

func TestAuthDisabledWithoutToken(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(cfg, nil, "")
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/roots")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("code = %d", resp.StatusCode)
	}
}
