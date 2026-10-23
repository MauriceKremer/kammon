// Package web holds KAMmon's routes, handlers and templates. Pages read the store, never KAM.
package web

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"kammon.bytecoder.nl/internal/auth"
	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
	"kammon.bytecoder.nl/internal/view"
	webassets "kammon.bytecoder.nl/web"
)

const (
	sessionCookie = "kammon_session"
	themeCookie   = "kammon_theme"
)

// AssetVersion is the cache-buster of the shared static assets (kammon.css and
// the static scripts). Bump it when one of those files changes, so browsers drop
// their cached copy instead of running or styling the pages with the old rules.
const AssetVersion = "kammon-10"

type Deps struct {
	Config config.Config
	Store  *store.Store
	Now    func() time.Time
	Demo   bool
	// KAM is the client for the admin's script calls; nil keeps the pages reading the store
	// and the editor saying it is not connected (AGENTS rule 3).
	KAM kam.Admin
	// Sleep is the pause login slowdowns impose (MON-AX-6); tests substitute it.
	Sleep func(time.Duration)
}

type Server struct {
	cfg      config.Config
	store    *store.Store
	now      func() time.Time
	demo     bool
	sessions *auth.Sessions
	slowdown *auth.Slowdown
	sleep    func(time.Duration)
	pages    map[string]*template.Template
	static   http.Handler
	// kam is the client of the script calls; pages never use it, only the automations admin
	// actions do (AGENTS rule 3).
	kam kam.Admin
}

// access is the role a route needs. There is no zero value, so a route cannot forget to say (MON-AX-11).
type access int

const (
	public access = iota + 1
	viewer
	admin
)

type route struct {
	pattern string
	access  access
	handler http.HandlerFunc
}

func New(d Deps) (*Server, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Sleep == nil {
		d.Sleep = time.Sleep
	}
	s := &Server{
		cfg: d.Config, store: d.Store, now: d.Now, demo: d.Demo, kam: d.KAM,
		sessions: auth.NewSessions(d.Config.SessionIdle(), d.Now),
		slowdown: auth.NewSlowdown(),
		sleep:    d.Sleep,
		pages:    map[string]*template.Template{},
	}
	base, err := template.ParseFS(webassets.FS, "templates/layout.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"overview", "placeholder", "login", "traffic", "channels", "rooms", "automations"} {
		clone, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if s.pages[name], err = clone.ParseFS(webassets.FS, "templates/pages/"+name+".html"); err != nil {
			return nil, err
		}
	}
	staticFS, err := fs.Sub(webassets.FS, "static")
	if err != nil {
		return nil, err
	}
	s.static = http.StripPrefix("/static/", http.FileServerFS(staticFS))
	return s, nil
}

func (s *Server) routes() []route {
	routes := []route{
		{"GET /static/", public, cachedFS(s.static)},
		{"GET /login", public, s.loginPage},
		{"POST /login", public, s.loginSubmit},
		{"POST /logout", viewer, s.logout},
		{"GET /{$}", viewer, s.overviewPage},
		{"GET /ui/overview", viewer, s.overviewPartial},
		{"GET /traffic", viewer, s.trafficPage},
		{"GET /ui/traffic", viewer, s.trafficPartial},
		{"GET /traffic.csv", viewer, s.csvPage},
		{"GET /channels", viewer, s.channelsPage},
		{"GET /ui/channel-messages", viewer, s.channelMessagesPartial},
		{"GET /ui/channel-metrics", viewer, s.channelMetricsPartial},
		{"GET /rooms", viewer, s.roomsPage},
		{"GET /ui/room-posts", admin, s.roomPostsPartial},
		{"GET /ui/room-metrics", viewer, s.roomMetricsPartial},
		{"GET /automations", viewer, s.automationsPage},
		{"GET /ui/script-list", viewer, s.scriptListPartial},
		{"POST /ui/script-validate", admin, s.scriptValidateSubmit},
		{"POST /ui/script-dry-run", admin, s.scriptDryRunSubmit},
		{"POST /ui/script-save", admin, s.scriptSaveSubmit},
		{"POST /ui/script-toggle", admin, s.scriptToggleSubmit},
		{"POST /ui/script-recover", admin, s.scriptRecoverSubmit},
		{"DELETE /ui/script", admin, s.scriptDeleteSubmit},
	}
	// Placeholder routes for the pages that are not built yet. Built pages register their own
	// routes above; the overview and traffic pages take themselves out of this loop.
	for _, p := range view.Pages(config.RoleAdmin) {
		if p.Key == "overview" || p.Key == "traffic" || p.Key == "channels" || p.Key == "rooms" || p.Key == "automations" {
			continue
		}
		level := viewer
		if p.Admin {
			level = admin
		}
		routes = append(routes, route{"GET " + p.Path, level, s.placeholder(p.Key)})
	}
	return routes
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routes() {
		mux.Handle(r.pattern, s.guard(r.access, r.handler))
	}
	return securityHeaders(mux)
}

// cachedFS lets shared caches (Cloudflare, browsers) hold static assets for
// four hours. Requests stay public GETs; the dynamic pages never carry this header.
func cachedFS(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=14400")
		h.ServeHTTP(w, r)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type principal struct {
	User config.User
	Role config.Role
}

type principalKey struct{}

func principalOf(r *http.Request) principal {
	p, _ := r.Context().Value(principalKey{}).(principal)
	return p
}

// guard enforces the role of a route on every request (MON-AX-2, MON-AX-8, MON-AX-11) and, for changes,
// that a browser-supplied Origin matches this host.
func (s *Server) guard(need access, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !sameOrigin(r) {
				http.Error(w, "Cross-site request refused.", http.StatusForbidden)
				return
			}
			// A state-changing route needs the htmx header on top of the Origin check (MON-AX-18);
			// pages that change state in a browser always carry it.
			if need != public && r.Header.Get("HX-Request") == "" {
				http.Error(w, "This route answers only to the in-page controls.", http.StatusBadRequest)
				return
			}
		}
		if need == public {
			h(w, r)
			return
		}
		user, ok := s.currentUser(r)
		if !ok {
			if r.Header.Get("HX-Request") != "" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// The session carries the role; KAMmon adds no transport rules, so the address that a
		// request comes from never changes what the role may do (MON-AX-8, D31).
		p := principal{User: user, Role: user.Role}
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
		if need == admin && p.Role != config.RoleAdmin {
			s.render(w, r, http.StatusForbidden, "placeholder", s.pageFor(r, "", "Not available", "This page is for the administrator.", placeholderData{Text: "You can look at the other pages, but this one is only available to the administrator."}))
			return
		}
		h(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

func (s *Server) currentUser(r *http.Request) (config.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return config.User{}, false
	}
	return s.sessions.Get(c.Value)
}

func secure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func themeOf(r *http.Request) string {
	if c, err := r.Cookie(themeCookie); err == nil && (c.Value == "dark" || c.Value == "light") {
		return c.Value
	}
	return ""
}

type loginData struct {
	Theme, Error, Hint string
	// StaticVersion is the static asset cache-buster, shared with the layout (AssetVersion).
	StaticVersion string
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, r, http.StatusOK, "")
}

func (s *Server) renderLogin(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := loginData{Theme: themeOf(r), Error: message, StaticVersion: AssetVersion}
	if s.demo {
		data.Hint = "Demo mode: admin / admin or viewer / viewer."
	}
	s.execute(w, status, s.pages["login"], "login", data)
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}
	// MON-AX-6: after five failed logins in a minute, the answer waits two seconds.
	if d := s.slowdown.Delay(auth.RemoteHost(r), s.now()); d > 0 {
		s.sleep(d)
	}
	user, ok := auth.Check(s.cfg.Users, r.PostForm.Get("name"), r.PostForm.Get("password"))
	if !ok {
		s.slowdown.Fail(auth.RemoteHost(r), s.now())
		s.renderLogin(w, r, http.StatusUnauthorized, "Wrong name or password.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.sessions.Create(user), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure(r),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure(r)})
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type staleBar struct {
	Show, Down bool
	Text       string
}

type nodeBlock struct {
	Name, LinkText, LinkClass, Frequency, Radio, Uptime, Mode string
	KamTime, ClockNote                                        string
}

type placeholderData struct{ Text, Slice, Doc string }

// page is everything the layout needs.
type page struct {
	Title, Subtitle, Active string
	Nav                     []view.Page
	UserName, RoleLabel     string
	Theme                   string
	// StaticVersion is the static asset cache-buster (AssetVersion); the login
	// page links the same version, tests keep them equal.
	StaticVersion string
	Stale         staleBar
	Node          nodeBlock
	Content       any
}

func (s *Server) pageFor(r *http.Request, active, title, subtitle string, content any) page {
	p := principalOf(r)
	pg := page{
		Title: title, Subtitle: subtitle, Active: active, Nav: view.Pages(p.Role),
		UserName: p.User.Name, RoleLabel: "Viewer", Theme: themeOf(r),
		StaticVersion: AssetVersion,
		Stale:         s.staleBar(), Node: s.nodeBlock(), Content: content,
	}
	if p.Role == config.RoleAdmin {
		pg.RoleLabel = "Admin"
	}
	return pg
}

func (s *Server) staleBar() staleBar {
	now := s.now()
	f, last := s.store.Freshness(now)
	switch {
	case f == store.Fresh:
		return staleBar{}
	case last.IsZero():
		return staleBar{Show: true, Down: true, Text: "Waiting for KAM."}
	default:
		return staleBar{Show: true, Down: f == store.Down, Text: fmt.Sprintf("KAM has not answered for %s. Showing data from %s.", shortAge(now.Sub(last)), last.Local().Format("15:04:05"))}
	}
}

func shortAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(d/time.Second))
	}
	return fmt.Sprintf("%d min", int(d/time.Minute))
}

func (s *Server) nodeBlock() nodeBlock {
	now := s.now()
	st, _ := s.store.Status()
	// Link truth lives in the status snapshot: a stream of events alone proves KAM's ring, not
	// that the modem and duty cycle are current (MON-DS-13 per snapshot, MON-DS-17).
	f, _ := s.store.SnapshotFreshness("status", now)
	nb := nodeBlock{Name: "KAM", LinkText: "Waiting for KAM", Frequency: "Freq.: —"}
	if s.demo {
		nb.Mode = "DEMO · GENERATED DATA"
	}
	if st.Epoch == "" {
		nb.LinkClass = "down"
		return nb
	}
	if st.NodeName != "" {
		nb.Name = st.NodeName
	}
	nb.LinkText = "KAM connected"
	switch f {
	case store.Stale:
		nb.LinkClass, nb.LinkText = "stale", "KAM slow to answer"
	case store.Down:
		nb.LinkClass, nb.LinkText = "down", "KAM not answering"
	}
	nb.Frequency = fmt.Sprintf("Freq.: %.3f MHz", float64(st.Radio.FrequencyHz)/1e6)
	nb.Radio = fmt.Sprintf("SF%d / BW%s / CR%d", st.Radio.SpreadingFactor, bandkHz(st.Radio.BandwidthHz), st.Radio.CodingRate)
	nb.Uptime = uptime(st.Now.Sub(st.StartedAt))
	// KAM time on every screen: our clock corrected by the measured offset (MON-DS-15).
	if !st.Now.IsZero() {
		off := s.store.ClockOffset()
		nb.KamTime = now.Add(off).Local().Format("15:04:05")
		if a := absDuration(off); a > 2*time.Second {
			direction := "behind"
			if off > 0 {
				direction = "ahead of"
			}
			nb.ClockNote = fmt.Sprintf("KAM clock %s %s this computer", shortAge(a), direction)
		}
	}
	return nb
}

// bandkHz turns the stored Hertz into kHz for the radio summary (contract §5.2:
// KAMmon converts for display). 62500 Hz is "62.5", 125000 Hz is "125": whole
// values keep no trailing decimal.
func bandkHz(hz uint32) string {
	return strconv.FormatFloat(float64(hz)/1000, 'f', -1, 64)
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (s *Server) overviewPage(w http.ResponseWriter, r *http.Request) {
	data := s.overview(principalOf(r).Role, r.URL.Query().Get("range"))
	s.render(w, r, http.StatusOK, "overview", s.pageFor(r, "overview", "Network overview", "Observed packets, airtime and rooms for this node", data))
}

// overviewPartial is what htmx fetches every 2 seconds (MON-OV-11). The stale bar and node block ride along out of band.
func (s *Server) overviewPartial(w http.ResponseWriter, r *http.Request) {
	data := s.overview(principalOf(r).Role, r.URL.Query().Get("range"))
	var buf bytes.Buffer
	t := s.pages["overview"]
	for _, part := range []struct {
		name string
		data any
	}{{"overview-live", data}, {"stalebar", s.staleBar()}, {"nodeblock", s.nodeBlock()}} {
		if err := t.ExecuteTemplate(&buf, part.name, part.data); err != nil {
			http.Error(w, "Template error.", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = buf.WriteTo(w)
}

var placeholders = map[string]placeholderData{
	"channels": {Text: "Channel activity and decoded messages.", Slice: "slice 3", Doc: "docs/requirements/Channels.md"},
	"rooms":    {Text: "Room activity, remembered clients and posts.", Slice: "slice 3", Doc: "docs/requirements/Rooms.md"},
	"accounts": {Text: "Room roles for the administrator.", Slice: "slice 6", Doc: "docs/requirements/RoomAccounts.md"},
	"modem":    {Text: "KISS modem snapshot, queries and radio settings.", Slice: "slice 5", Doc: "docs/requirements/KissModem.md"},
}

func (s *Server) placeholder(key string) http.HandlerFunc {
	p, _ := view.Find(key)
	return func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusOK, "placeholder", s.pageFor(r, key, p.Title, "Not built yet", placeholders[key]))
	}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, pg page) {
	s.execute(w, status, s.pages[name], "layout", pg)
}

// execute renders into a buffer first so a template error never sends half a page.
func (s *Server) execute(w http.ResponseWriter, status int, t *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Template error.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
