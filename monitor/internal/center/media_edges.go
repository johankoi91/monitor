package center

import (
	"errors"
	"net"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

// MediaEdges rebuilds the grouped map from the last reported container identities.
// It includes unmanaged containers and never appends repeated observations to a group.
func (s *Store) MediaEdges() model.MediaEdgeInventory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := model.MediaEdgeInventory{ByAP: map[string][]model.MediaEdgeEndpoint{}, Hosts: []string{}, Skipped: []model.MediaEdgeSkipped{}, GeneratedAt: time.Now().UTC()}
	hosts := map[string]bool{}
	for _, agent := range s.config.Agents {
		if agent.HostAddress != "" {
			hosts[agent.HostAddress] = true
		}
		state := s.agents[agent.ID]
		if state == nil {
			continue
		}
		for _, observation := range state.Report.Containers {
			container := observation.Container
			if !strings.Contains(strings.ToLower(container.ContainerName), "media_edge") {
				continue
			}
			out.MatchedContainers++
			skip := func(reason string) {
				out.Skipped = append(out.Skipped, model.MediaEdgeSkipped{AgentID: agent.ID, HostIP: agent.HostAddress, ContainerName: container.ContainerName, Reason: reason})
			}
			kind := mediaEdgeType(container.ContainerName)
			if kind == "" {
				skip("UNKNOWN_EDGE_TYPE")
				continue
			}
			record, ok := s.lastStartups[agent.ID][container.ContainerName]
			if !ok || record.Startup == nil {
				skip("WAITING_STARTUP")
				continue
			}
			if record.ContainerID != container.ContainerID || record.Startup.SnapshotID != container.SnapshotID {
				skip("STARTUP_IDENTITY_MISMATCH")
				continue
			}
			if record.Startup.Incomplete || record.Startup.Truncated {
				skip("STARTUP_INCOMPLETE")
				continue
			}
			ap, ip, port, err := mediaEdgeArguments(record.Startup.Config)
			if err != nil {
				skip(err.Error())
				continue
			}
			stale := s.startupViewLocked(agent.ID, container.ContainerName, &observation)["stale"].(bool)
			for _, group := range strings.Split(ap, ",") {
				out.ByAP[group] = append(out.ByAP[group], model.MediaEdgeEndpoint{EdgeType: kind, ContainerID: container.ContainerID, SnapshotID: container.SnapshotID, IPForClient: ip, UserPort: port, AgentID: agent.ID, HostIP: agent.HostAddress, ContainerName: container.ContainerName, Stale: stale})
			}
		}
	}
	for host := range hosts {
		out.Hosts = append(out.Hosts, host)
	}
	sort.Strings(out.Hosts)
	for ap := range out.ByAP {
		rows := out.ByAP[ap]
		sort.Slice(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.HostIP != b.HostIP {
				return a.HostIP < b.HostIP
			}
			if a.UserPort != b.UserPort {
				return a.UserPort < b.UserPort
			}
			if a.ContainerName != b.ContainerName {
				return a.ContainerName < b.ContainerName
			}
			return a.AgentID < b.AgentID
		})
	}
	sort.Slice(out.Skipped, func(i, j int) bool {
		a, b := out.Skipped[i], out.Skipped[j]
		return a.AgentID+"/"+a.ContainerName < b.AgentID+"/"+b.ContainerName
	})
	return out
}

func mediaEdgeArguments(config model.StartupConfig) (string, string, int, error) {
	args := config.Args
	if len(args) == 0 {
		args = append(append([]string(nil), config.Entrypoint...), config.Cmd...)
	}
	// Docker Args is authoritative. Only a shell's command argument needs tokenizing.
	previous := filepath.Base(config.Path)
	for i, arg := range args {
		shell := previous == "sh" || previous == "bash" || previous == "dash" || previous == "ash"
		if shell && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "c") && i+1 < len(args) {
			parsed, err := commandWords(args[i+1])
			if err != nil {
				return "", "", 0, errors.New("INVALID_COMMAND")
			}
			args = parsed
			break
		}
		previous = filepath.Base(arg)
	}
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		name, value, inline := strings.Cut(arg, "=")
		if name != "--ap" && name != "--ip-for-client" && name != "--user-port" {
			continue
		}
		if !inline {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return "", "", 0, errors.New("MISSING_ARGUMENT_VALUE")
			}
			i++
			value = args[i]
		}
		if old, exists := values[name]; exists && old != value {
			return "", "", 0, errors.New("AMBIGUOUS_ARGUMENT")
		}
		values[name] = value
	}
	groups := []string{}
	seen := map[string]bool{}
	for _, value := range strings.Split(values["--ap"], ",") {
		ap := net.ParseIP(strings.TrimSpace(value))
		if ap == nil {
			return "", "", 0, errors.New("MISSING_OR_INVALID_AP")
		}
		if !seen[ap.String()] {
			groups = append(groups, ap.String())
			seen[ap.String()] = true
		}
	}
	sort.Strings(groups)
	ip := net.ParseIP(values["--ip-for-client"])
	if ip == nil {
		return "", "", 0, errors.New("MISSING_OR_INVALID_CLIENT_IP")
	}
	port, err := strconv.Atoi(values["--user-port"])
	if err != nil || port < 1 || port > 65535 {
		return "", "", 0, errors.New("MISSING_OR_INVALID_USER_PORT")
	}
	return strings.Join(groups, ","), ip.String(), port, nil
}

// Tokenizes literal shell words; substitutions, variables and commands are not run.
func commandWords(command string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, char := range command {
		if escaped {
			word.WriteRune(char)
			escaped = false
			started = true
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
			started = true
			continue
		}
		if strings.ContainsRune(";&|<>`$", char) || char == '#' && !started {
			return nil, errors.New("unsupported shell expression")
		}
		if char == '\'' || char == '"' {
			quote = char
			started = true
			continue
		}
		if unicode.IsSpace(char) {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		} else {
			word.WriteRune(char)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("unterminated command word")
	}
	if started {
		words = append(words, word.String())
	}
	return words, nil
}

var mediaEdgeTypes = regexp.MustCompile(`(?:^|_)(udp|aut|web)_media_edge(?:_|$)`)

func mediaEdgeType(name string) string {
	match := mediaEdgeTypes.FindStringSubmatch(strings.ToLower(name))
	if len(match) != 2 {
		return ""
	}
	return match[1]
}
