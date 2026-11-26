package web

import (
	"net/http"
	"slices"
	"sync"

	"kammon.bytecoder.nl/internal/audit"
	"kammon.bytecoder.nl/internal/auth"
	"kammon.bytecoder.nl/internal/config"
)

type userManager struct {
	mu         sync.Mutex
	configPath string
	demo       bool
	users      []config.User
}

func newUserManager(configPath string, demo bool, initial []config.User) *userManager {
	// Make a shallow copy of initial users
	cp := make([]config.User, len(initial))
	copy(cp, initial)
	return &userManager{
		configPath: configPath,
		demo:       demo,
		users:      cp,
	}
}

func (m *userManager) list() []config.User {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]config.User, len(m.users))
	copy(cp, m.users)
	return cp
}

func (m *userManager) save(origName, name string, role config.Role, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]config.User, len(m.users))
	copy(cp, m.users)

	idx := slices.IndexFunc(cp, func(u config.User) bool { return u.Name == origName })
	if origName != "" && idx >= 0 {
		// Editing existing user
		user := cp[idx]
		user.Name = name
		user.Role = role
		if password != "" {
			hash, err := auth.HashPassword(password)
			if err != nil {
				return err
			}
			user.Password = hash
		}
		cp[idx] = user
	} else {
		// Adding new user
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		cp = append(cp, config.User{Name: name, Password: hash, Role: role})
	}

	if err := config.ValidateUsers(cp); err != nil {
		return err
	}

	if !m.demo && m.configPath != "" {
		if err := config.UpdateUsersFile(m.configPath, cp); err != nil {
			return err
		}
	}

	m.users = cp
	return nil
}

func (m *userManager) delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := slices.IndexFunc(m.users, func(u config.User) bool { return u.Name == name })
	if idx < 0 {
		return nil
	}

	cp := make([]config.User, 0, len(m.users)-1)
	cp = append(cp, m.users[:idx]...)
	cp = append(cp, m.users[idx+1:]...)

	if err := config.ValidateUsers(cp); err != nil {
		return err
	}

	if !m.demo && m.configPath != "" {
		if err := config.UpdateUsersFile(m.configPath, cp); err != nil {
			return err
		}
	}

	m.users = cp
	return nil
}

type webUsersData struct {
	Users   []config.User
	Form    userForm
	Editing bool
	Demo    bool
	Error   string
	Notice  string
	Warn    string
}

type userForm struct {
	Name string
	Role string
}

func (s *Server) webUsersPage(w http.ResponseWriter, r *http.Request) {
	editName := r.URL.Query().Get("edit")
	users := s.userMgr.list()

	data := webUsersData{
		Users: users,
		Demo:  s.demo,
	}

	if editName != "" {
		for _, u := range users {
			if u.Name == editName {
				data.Editing = true
				data.Form = userForm{Name: u.Name, Role: string(u.Role)}
				break
			}
		}
	}

	s.render(w, r, http.StatusOK, "web-users", s.pageFor(r, "web-users", "Web users", "Manage KAMmon logins", data))
}

func (s *Server) webUsersSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	origName := r.PostForm.Get("orig_name")
	name := r.PostForm.Get("name")
	role := config.Role(r.PostForm.Get("role"))
	password := r.PostForm.Get("password")

	before := s.findUser(origName)
	err := s.userMgr.save(origName, name, role, password)
	if err != nil {
		s.recordUserChange(r, origName, name, role, before, password, err)
		users := s.userMgr.list()
		data := webUsersData{
			Users:   users,
			Demo:    s.demo,
			Editing: origName != "",
			Form:    userForm{Name: name, Role: string(role)},
			Error:   err.Error(),
		}
		s.render(w, r, http.StatusUnprocessableEntity, "web-users", s.pageFor(r, "web-users", "Web users", "Manage KAMmon logins", data))
		return
	}
	warn := s.recordUserChange(r, origName, name, role, before, password, nil)

	// Invalidate modified user's sessions (MON-WA-11).
	// Skip if the acting admin edited their own account; they keep their session active.
	acting := principalOf(r)
	target := origName
	if target == "" {
		target = name
	}
	if acting.User.Name != target {
		s.sessions.RevokeUser(target)
	}
	if name != target && acting.User.Name != name {
		s.sessions.RevokeUser(name)
	}

	if warn != "" {
		// Show the warning in place instead of redirecting, so the admin sees it (MON-AT-19).
		data := webUsersData{Users: s.userMgr.list(), Demo: s.demo, Warn: warn}
		s.render(w, r, http.StatusOK, "web-users", s.pageFor(r, "web-users", "Web users", "Manage KAMmon logins", data))
		return
	}

	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", "/web-users")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/web-users", http.StatusSeeOther)
}

func (s *Server) webUsersDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if err := s.userMgr.delete(name); err != nil {
		s.recordUserDelete(r, name, err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	warn := s.recordUserDelete(r, name, nil)

	// Invalidate deleted user's sessions (MON-WA-11).
	// Skip if the acting admin deleted their own account; shouldn't happen (validation prevents zero admins),
	// but be defensive. Admin will be signed out on next request.
	acting := principalOf(r)
	if acting.User.Name != name {
		s.sessions.RevokeUser(name)
	}

	data := webUsersData{
		Users: s.userMgr.list(),
		Demo:  s.demo,
		Warn:  warn,
	}
	s.execute(w, http.StatusOK, s.pages["web-users"], "users-table", data)
}

// findUser returns a copy of the named user, or nil when there is none (for the audit record's
// before values).
func (s *Server) findUser(name string) *config.User {
	if name == "" {
		return nil
	}
	for _, u := range s.userMgr.list() {
		if u.Name == name {
			uu := u
			return &uu
		}
	}
	return nil
}

// recordUserChange writes the audit record of an add or edit (MON-AT-1, 4, 5). It never stores
// the password, only that one was set or changed.
func (s *Server) recordUserChange(r *http.Request, origName, name string, role config.Role, before *config.User, password string, actionErr error) string {
	action := "user.update"
	target := &audit.Target{Type: "user", Name: name}
	changes := map[string]audit.Change{}
	if origName == "" {
		action = "user.create"
		changes["name"] = audit.Change{To: name}
		changes["role"] = audit.Change{To: string(role)}
		changes["password"] = audit.Change{To: "<set>"}
	} else {
		if before != nil {
			if before.Name != name {
				changes["name"] = audit.Change{From: before.Name, To: name}
			}
			if before.Role != role {
				changes["role"] = audit.Change{From: string(before.Role), To: string(role)}
			}
		}
		if password != "" {
			changes["password"] = audit.Change{To: "<changed>"}
		}
	}
	outcome, detail := audit.OK, ""
	switch {
	case actionErr != nil:
		outcome, detail = audit.Failed, actionErr.Error()
		changes = nil
	case action == "user.create":
		detail = "Created " + name + "."
	case len(changes) == 0:
		detail = "Saved " + name + " with no changes."
	default:
		detail = "Updated " + name + "."
	}
	return s.auditChange(r, action, target, changes, outcome, detail)
}

// recordUserDelete writes the audit record of a delete (MON-AT-1).
func (s *Server) recordUserDelete(r *http.Request, name string, actionErr error) string {
	outcome, detail := audit.OK, "Deleted "+name+"."
	if actionErr != nil {
		outcome, detail = audit.Failed, actionErr.Error()
	}
	return s.auditChange(r, "user.delete", &audit.Target{Type: "user", Name: name}, nil, outcome, detail)
}
