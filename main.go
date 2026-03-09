package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
)

type busFrame struct {
	Type      string `json:"type,omitempty"`
	From      string `json:"from,omitempty"`
	Target    string `json:"target,omitempty"`
	Room      string `json:"room,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	Message   string `json:"message,omitempty"`
	Command   string `json:"command,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

func main() {
	var (
		upstream       = flag.String("upstream", "nats://127.0.0.1:4222", "Upstream NATS URL")
		subjectsCSV    = flag.String("subjects", "repl.>", "Comma-separated subject patterns")
		name           = flag.String("name", "dialtone-tap", "NATS client name")
		reconnectWait  = flag.Duration("reconnect-wait", 1200*time.Millisecond, "Reconnect wait interval")
		showRaw        = flag.Bool("raw", false, "Print raw payloads instead of REPL-style formatting")
		showSubject    = flag.Bool("show-subject", true, "Prefix output lines with subject")
		showReconnects = flag.Bool("show-reconnect-events", true, "Print reconnect lifecycle events")
	)
	flag.Parse()

	subjects := parseSubjects(*subjectsCSV)
	if len(subjects) == 0 {
		fatalf("no subjects configured")
	}

	opts := []nats.Option{
		nats.Name(strings.TrimSpace(*name)),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(*reconnectWait),
		nats.Timeout(1200 * time.Millisecond),
	}
	if *showReconnects {
		opts = append(opts,
			nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "TAP> disconnected: %v\n", err)
				} else {
					fmt.Fprintln(os.Stderr, "TAP> disconnected")
				}
			}),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				fmt.Fprintf(os.Stderr, "TAP> reconnected: %s\n", nc.ConnectedUrl())
			}),
			nats.ClosedHandler(func(_ *nats.Conn) {
				fmt.Fprintln(os.Stderr, "TAP> connection closed")
			}),
			nats.DiscoveredServersHandler(func(nc *nats.Conn) {
				if len(nc.DiscoveredServers()) > 0 {
					fmt.Fprintf(os.Stderr, "TAP> discovered servers: %s\n", strings.Join(nc.DiscoveredServers(), ", "))
				}
			}),
		)
	}

	fmt.Fprintf(os.Stderr, "TAP> connecting to %s (subjects=%s)\n", strings.TrimSpace(*upstream), strings.Join(subjects, ","))
	nc, err := nats.Connect(strings.TrimSpace(*upstream), opts...)
	if err != nil {
		fatalf("initial connect failed: %v", err)
	}
	defer nc.Close()
	fmt.Fprintf(os.Stderr, "TAP> connected: %s\n", nc.ConnectedUrl())

	for _, subject := range subjects {
		subj := subject
		_, subErr := nc.Subscribe(subj, func(msg *nats.Msg) {
			printMessage(msg, *showRaw, *showSubject)
		})
		if subErr != nil {
			fatalf("subscribe failed for %q: %v", subj, subErr)
		}
	}
	if err := nc.Flush(); err != nil {
		fatalf("nats flush failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	_ = nc.Drain()
}

func parseSubjects(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func printMessage(msg *nats.Msg, raw bool, showSubject bool) {
	subject := strings.TrimSpace(msg.Subject)
	if raw {
		line := strings.TrimSpace(string(msg.Data))
		if showSubject {
			fmt.Printf("[%s] %s\n", subject, line)
		} else {
			fmt.Println(line)
		}
		return
	}

	if strings.HasPrefix(subject, "repl.") {
		var frame busFrame
		if err := json.Unmarshal(msg.Data, &frame); err == nil {
			printFrame(subject, frame, showSubject)
			return
		}
	}

	line := strings.TrimSpace(string(msg.Data))
	if showSubject {
		fmt.Printf("[%s] %s\n", subject, line)
	} else {
		fmt.Println(line)
	}
}

func printFrame(subject string, f busFrame, showSubject bool) {
	prefix := ""
	if showSubject {
		prefix = "[" + subject + "] "
	}
	switch strings.TrimSpace(f.Type) {
	case "input":
		user := firstNonEmpty(f.From, "unknown")
		fmt.Printf("%s%s> %s\n", prefix, user, strings.TrimSpace(f.Message))
	case "line":
		p := firstNonEmpty(f.Prefix, "DIALTONE")
		fmt.Printf("%s%s> %s\n", prefix, p, strings.TrimSpace(f.Message))
	case "chat":
		user := firstNonEmpty(f.From, "chat")
		fmt.Printf("%s%s: %s\n", prefix, user, strings.TrimSpace(f.Message))
	case "join":
		fmt.Printf("%sDIALTONE> [JOIN] %s (room=%s)\n", prefix, firstNonEmpty(f.From, "unknown"), firstNonEmpty(f.Room, "index"))
	case "left":
		fmt.Printf("%sDIALTONE> [LEFT] %s (room=%s)\n", prefix, firstNonEmpty(f.From, "unknown"), firstNonEmpty(f.Room, "index"))
	case "server":
		fmt.Printf("%sDIALTONE> %s\n", prefix, strings.TrimSpace(f.Message))
	case "error":
		fmt.Printf("%sDIALTONE> [ERROR] %s\n", prefix, strings.TrimSpace(f.Message))
	default:
		if strings.TrimSpace(f.Message) != "" {
			fmt.Printf("%s[%s] %s\n", prefix, firstNonEmpty(f.Type, "frame"), strings.TrimSpace(f.Message))
		} else {
			fmt.Printf("%s[%s] from=%s room=%s command=%s\n", prefix, firstNonEmpty(f.Type, "frame"), strings.TrimSpace(f.From), strings.TrimSpace(f.Room), strings.TrimSpace(f.Command))
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "TAP> ERROR: "+format+"\n", args...)
	os.Exit(1)
}
