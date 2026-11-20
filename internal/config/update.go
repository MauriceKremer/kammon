package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ValidateUsers checks user list invariants (MON-WA-7..10).
func ValidateUsers(users []User) error {
	if len(users) == 0 {
		return fmt.Errorf("at least one user is required")
	}
	seen := map[string]bool{}
	adminCount := 0
	for i, u := range users {
		switch {
		case u.Name == "":
			return fmt.Errorf("users[%d]: name is required", i)
		case u.Password == "":
			return fmt.Errorf("users[%d] (%s): password is required", i, u.Name)
		case u.Role != RoleViewer && u.Role != RoleAdmin:
			return fmt.Errorf("users[%d] (%s): role must be viewer or admin, got %q", i, u.Name, u.Role)
		case seen[u.Name]:
			return fmt.Errorf("users[%d]: duplicate name %q", i, u.Name)
		}
		seen[u.Name] = true
		if u.Role == RoleAdmin {
			adminCount++
		}
	}
	if adminCount == 0 {
		return fmt.Errorf("at least one administrator is required")
	}
	return nil
}

// UpdateUsersFile updates the `users:` section in the given YAML config file,
// preserving other sections, values and comments as much as possible (MON-WA-6).
// It parses the YAML into an AST, finds or creates the `users:` key in the root mapping,
// replaces its value node with the new marshaled users, and writes back with the original
// formatting preserved by yaml.v3.
func UpdateUsersFile(path string, users []User) error {
	if err := ValidateUsers(users); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: root must be a mapping", path)
	}
	rootMap := doc.Content[0]

	// Marshal new users into a yaml.Node
	var usersNode yaml.Node
	rawUsers, err := yaml.Marshal(users)
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}
	var usersDoc yaml.Node
	if err := yaml.Unmarshal(rawUsers, &usersDoc); err != nil {
		return fmt.Errorf("unmarshal users node: %w", err)
	}
	if usersDoc.Kind == yaml.DocumentNode && len(usersDoc.Content) > 0 {
		usersNode = *usersDoc.Content[0]
	} else {
		usersNode = usersDoc
	}

	// Find "users" in rootMap
	found := false
	for i := 0; i < len(rootMap.Content); i += 2 {
		keyNode := rootMap.Content[i]
		if keyNode.Value == "users" {
			rootMap.Content[i+1] = &usersNode
			found = true
			break
		}
	}
	if !found {
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: "users",
		}
		rootMap.Content = append(rootMap.Content, keyNode, &usersNode)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	_ = enc.Close()

	// Atomic write via temp file
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "kammon-cfg-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(buf.Bytes()); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	return nil
}
