package web

import (
	"net/http"
	"slices"
	"sync"

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

	err := s.userMgr.save(origName, name, role, password)
	if err != nil {
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
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

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
	}
	s.execute(w, http.StatusOK, s.pages["web-users"], "users-table", data)
}
