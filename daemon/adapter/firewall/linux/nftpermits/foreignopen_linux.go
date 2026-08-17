package nftpermits

// The FOREIGN firewall gate: look whether it blocks, open it with consent, and
// undo exactly what was opened.
//
// # This derogates an old rule of this package, and says so
//
// The rule was "lo ajeno se reporta y jamás se toca". Its physical half is
// still true: our `accept` cannot beat a ufw `drop` from our own table,
// because in netfilter a drop ends the evaluation of the whole hook. What
// changes is the POLICY half: asking the operator's manager to let our
// adapters through, with ITS own CLI, is possible, and refusing to do it even
// with explicit consent produced the defect measured on 2026-08-16: a
// perfectly assembled room, everything green, and nobody gets in. Decision 36
// writes the full derogation; what this file needs to be readable is:
//
//   - Only the commands that were SHOWN to the person run, from a closed
//     list. Nothing is interpolated from the outside.
//   - Whatever gets added is written down in a book, with a date. Undo removes
//     what the book says and nothing else: a rule the operator already had
//     never enters the book, so it can never leave through it. This is the
//     regime of decision 29.
//   - Without consent nothing runs: this file does not decide, it obeys a use
//     case that already asked.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/safewrite"
)

// AllowsBook is the ledger of what Kanpachi opened in the operator's firewall.
//
// It lives with the rest of the daemon's state and not under /etc: it is not
// configuration, it is the memory of a debt. While it has entries there is an
// open door that is ours to close, and the service start reads it in case a
// dirty death left the debt unpaid.
const AllowsBook = "/var/lib/kanpachi/foreign-allows.json"

// allowEntry is one line of the book: which manager, which adapter, since when.
type allowEntry struct {
	Manager string    `json:"manager"`
	Adapter string    `json:"adapter"`
	Since   time.Time `json:"since"`
}

// opener is a manager we know how to open: how it is shown, opened and closed.
type opener struct {
	name string
	unit string
	// shown is the command displayed to the person, with the adapter as %s.
	// It has to say THE SAME thing `open` does: the consent is about this.
	shown string
	open  func(ctx context.Context, adapter string) error
	close func(ctx context.Context, adapter string) error
}

// openers is the CLOSED list of managers we know how to open.
//
// Closed for the same reason as `gestores`: recognizing one too many produces
// commands the operator cannot verify. Arguments are LITERAL: the only thing
// interpolated is the adapter name, which is a domain constant
// ([domain.AdapterName], [domain.LobbyAdapterName]).
var openers = []opener{
	{
		name:  "ufw",
		unit:  "ufw.service",
		shown: "sudo ufw allow in on %s",
		open: func(ctx context.Context, adapter string) error {
			return runFirewallCmd(ctx, "ufw", "allow", "in", "on", adapter,
				"comment", "opened by Kanpachi, removed when the room closes")
		},
		close: func(ctx context.Context, adapter string) error {
			return runFirewallCmd(ctx, "ufw", "delete", "allow", "in", "on", adapter)
		},
	},
	{
		name:  "firewalld",
		unit:  "firewalld.service",
		shown: "sudo firewall-cmd --zone=trusted --add-interface=%s --permanent && sudo firewall-cmd --reload",
		open: func(ctx context.Context, adapter string) error {
			if err := runFirewallCmd(ctx, "firewall-cmd", "--zone=trusted",
				"--add-interface="+adapter, "--permanent"); err != nil {
				return err
			}
			return runFirewallCmd(ctx, "firewall-cmd", "--reload")
		},
		close: func(ctx context.Context, adapter string) error {
			if err := runFirewallCmd(ctx, "firewall-cmd", "--zone=trusted",
				"--remove-interface="+adapter, "--permanent"); err != nil {
				return err
			}
			return runFirewallCmd(ctx, "firewall-cmd", "--reload")
		},
	},
}

// roomAdapters are the two adapters ALWAYS asked about.
//
// Both, not per-role: the host lives on both, and the guest passes through the
// lobby on the way in. Asking about the one that "does not apply" opens
// nothing extra, because opening is another call, and consented.
func roomAdapters() []string {
	return []string{domain.AdapterName, domain.LobbyAdapterName}
}

// InboundBlocked looks whether a foreign manager will swallow our inbound.
//
// # This one DOES interpret policy, and blockingManagers still does not
//
// They are two different questions on purpose. The blockingManagers alert says
// "someone else governs this machine's firewall", always while active, because
// its policy can change tomorrow. This one answers something else: TODAY, will
// this manager kill the room? That requires reading its posture, and ties go
// to the pessimistic side: whatever cannot be read counts as blocking, because
// saying "clear" without knowing is the original defect in new clothes.
func (p *Permits) InboundBlocked(ctx context.Context) ([]domain.FirewallBlock, error) {
	var out []domain.FirewallBlock
	for _, g := range openers {
		block, err := inboundBlockOf(ctx, g)
		if err != nil {
			return nil, err
		}
		if block != nil {
			out = append(out, *block)
		}
	}
	return out, nil
}

// inboundBlockOf builds the block of ONE manager, or nil if it does not block.
func inboundBlockOf(ctx context.Context, g opener) (*domain.FirewallBlock, error) {
	switch g.name {
	case "ufw":
		return ufwBlock(ctx, g.shown)
	case "firewalld":
		return firewalldBlock(ctx, g.unit, g.shown)
	default:
		// The list is closed; reaching this is a programming error, not the
		// operator's. Better an error than a manager silently unexamined.
		return nil, fmt.Errorf("no way to read the posture of %s", g.name)
	}
}

// ufwDefaultAllow recognizes the "Default: allow (incoming), ..." line of the
// verbose status, which is the ONLY posture that exempts: deny, reject and
// anything unreadable all count as blocking.
var ufwDefaultAllow = regexp.MustCompile(`Default:\s*allow\s*\(incoming\)`)

// ufwBlock decides whether ufw will swallow inbound on our adapters.
//
// `ufw status verbose` is read ONCE and all three answers come from it:
// whether it is active, its inbound posture, and which adapters already have a
// pass. The format is what the operator sees running the same command; the
// registry's tests already pinned its variants ("(v6)", LIMIT) and here only
// the `ALLOW IN` + `on <adapter>` shape of per-interface rules matters.
func ufwBlock(ctx context.Context, shown string) (*domain.FirewallBlock, error) {
	if _, err := exec.LookPath("ufw"); err != nil {
		return nil, nil // ufw not installed: nothing to look at
	}
	raw, err := exec.CommandContext(ctx, "ufw", "status", "verbose").CombinedOutput()
	if err != nil {
		// No inventing "clear": reading badly and saying nothing is wrong IS
		// the original defect. The normal case (daemon as root) can read.
		return nil, fmt.Errorf("could not read the ufw posture: %w: %s",
			err, strings.TrimSpace(string(raw)))
	}
	status := string(raw)
	if !strings.Contains(status, "Status: active") {
		return nil, nil
	}
	if ufwDefaultAllow.MatchString(status) {
		// Inbound allowed by default: active, and still not killing the room.
		return nil, nil
	}
	// It denies inbound, or its posture could not be read. Both count as
	// blocking: ties go to the pessimistic side.
	return ufwAdapterBlock(shown, status), nil
}

// ufwAdapterBlock collects the adapters ufw does NOT let through, or nil if
// they all pass.
func ufwAdapterBlock(shown, status string) *domain.FirewallBlock {
	b := domain.FirewallBlock{Manager: "ufw"}
	for _, adapter := range roomAdapters() {
		if ufwAllowsAdapter(status, adapter) {
			continue
		}
		b.Adapters = append(b.Adapters, adapter)
		b.Fix = append(b.Fix, fmt.Sprintf(shown, adapter))
	}
	if len(b.Adapters) == 0 {
		return nil
	}
	return &b
}

// ufwAllowsAdapter looks for a per-interface rule that lets inbound through.
//
// The shape is "Anywhere on kanpachi0   ALLOW IN   Anywhere" in the verbose
// status. LIMIT also lets through (limited is open), same as the registry's
// parser. The FULL name with a delimiter is required: kanpachi1 must not match
// inside kanpachi10.
func ufwAllowsAdapter(status, adapter string) bool {
	for _, line := range strings.Split(status, "\n") {
		if !strings.Contains(line, "ALLOW IN") && !strings.Contains(line, "LIMIT IN") {
			continue
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Anywhere on "+adapter)
		if ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			return true
		}
	}
	return false
}

// firewalldBlock decides whether firewalld will swallow inbound.
//
// The readable question here is whether the adapter is in the trusted zone;
// firewalld's default posture denies the unsolicited, so active and outside
// the zone counts as blocking. Knowingly the pessimistic side.
func firewalldBlock(ctx context.Context, unit, shown string) (*domain.FirewallBlock, error) {
	if !unitActive(ctx, unit) {
		return nil, nil
	}
	b := domain.FirewallBlock{Manager: "firewalld"}
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		// Unit active and no CLI to ask it: unreadable, so it blocks.
		for _, adapter := range roomAdapters() {
			b.Adapters = append(b.Adapters, adapter)
			b.Fix = append(b.Fix, fmt.Sprintf(shown, adapter))
		}
		return &b, nil
	}
	for _, adapter := range roomAdapters() {
		cmd := exec.CommandContext(ctx, "firewall-cmd", "--zone=trusted", "--query-interface="+adapter)
		if cmd.Run() == nil {
			continue // already in the trusted zone
		}
		b.Adapters = append(b.Adapters, adapter)
		b.Fix = append(b.Fix, fmt.Sprintf(shown, adapter))
	}
	if len(b.Adapters) == 0 {
		return nil, nil
	}
	return &b, nil
}

// AllowAdapters opens what is blocked, with consent ALREADY given.
//
// The use case already showed [domain.FirewallBlock.Fix] and the person said
// yes; this only runs that same thing and writes it down. What gets re-checked
// before opening is whether the adapter is still blocked: whatever already
// passes is neither touched nor booked, which is why undo can never take an
// operator's rule away.
func (p *Permits) AllowAdapters(ctx context.Context, blocks []domain.FirewallBlock) error {
	if len(blocks) == 0 {
		return nil
	}
	book, err := readBook()
	if err != nil {
		return err
	}
	for _, b := range blocks {
		g, ok := openerFor(b.Manager)
		if !ok {
			return fmt.Errorf("no way to open %s: not in the list of known managers", b.Manager)
		}
		for _, adapter := range b.Adapters {
			if !isOurAdapter(adapter) {
				// Adapter names are domain constants. A different one here is
				// someone trying to use this path to open something else.
				return fmt.Errorf("%q is not a Kanpachi adapter, and nothing opens for it", adapter)
			}
			if booked(book, b.Manager, adapter) {
				continue // we already opened it; reopening would duplicate the rule
			}
			if err := g.open(ctx, adapter); err != nil {
				return fmt.Errorf("opening %s for %s: %w", b.Manager, adapter, err)
			}
			book = append(book, allowEntry{Manager: b.Manager, Adapter: adapter, Since: time.Now()})
			p.log.Info("firewall ajeno abierto con consentimiento",
				"gestor", b.Manager, "adaptador", adapter, "libro", AllowsBook)
		}
	}
	return writeBook(book)
}

// WithdrawAdapters closes what the book says, and nothing else.
//
// With an empty book it does nothing and does not fail: "leave the machine
// with no debts" is already true where nothing was ever opened, same contract
// as RestoreForeign. A close that fails leaves its entry IN the book and moves
// on to the rest: the debt is not erased by failing to pay it, and the next
// service start retries.
func (p *Permits) WithdrawAdapters(ctx context.Context) error {
	book, err := readBook()
	if err != nil {
		return err
	}
	if len(book) == 0 {
		return nil
	}
	var kept []allowEntry
	for _, e := range book {
		g, ok := openerFor(e.Manager)
		if !ok {
			// A book naming an unknown manager comes from a newer version.
			// The entry is kept: dropping it would be forgetting the debt.
			kept = append(kept, e)
			continue
		}
		if err := g.close(ctx, e.Adapter); err != nil {
			p.log.Warn("no se pudo cerrar lo abierto en el firewall ajeno; queda anotado",
				"gestor", e.Manager, "adaptador", e.Adapter, "error", err)
			kept = append(kept, e)
			continue
		}
		p.log.Info("cerrado lo que se abrió en el firewall ajeno",
			"gestor", e.Manager, "adaptador", e.Adapter)
	}
	return writeBook(kept)
}

func openerFor(name string) (opener, bool) {
	for _, g := range openers {
		if g.name == name {
			return g, true
		}
	}
	return opener{}, false
}

func isOurAdapter(adapter string) bool {
	return adapter == domain.AdapterName || adapter == domain.LobbyAdapterName
}

func booked(book []allowEntry, manager, adapter string) bool {
	for _, e := range book {
		if e.Manager == manager && e.Adapter == adapter {
			return true
		}
	}
	return false
}

// readBook reads the ledger, and an absent book is an empty book.
//
// An UNREADABLE book is an error instead: treating it as empty would forget
// debts exactly when something is wrong, which is when they matter most.
func readBook() ([]allowEntry, error) {
	raw, err := os.ReadFile(AllowsBook)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the allows book %s: %w", AllowsBook, err)
	}
	var book []allowEntry
	if err := json.Unmarshal(raw, &book); err != nil {
		return nil, fmt.Errorf("the allows book %s cannot be understood: %w", AllowsBook, err)
	}
	return book, nil
}

// writeBook writes the whole ledger, or removes it once there are no debts.
//
// Removing it matters: an empty book on disk reads the same as one with debts
// until opened, and the machine's resting state must be "no file", which is
// also what uninstalling leaves.
func writeBook(book []allowEntry) error {
	if len(book) == 0 {
		if err := os.Remove(AllowsBook); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing the paid-up allows book: %w", err)
		}
		return nil
	}
	raw, err := json.MarshalIndent(book, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(AllowsBook), 0o755); err != nil {
		return fmt.Errorf("creating the allows book folder: %w", err)
	}
	// Atomic and root-only: the book says which doors are open.
	if err := safewrite.File(AllowsBook, raw, 0o600); err != nil {
		return fmt.Errorf("writing the allows book: %w", err)
	}
	return nil
}

// runFirewallCmd invokes a manager's CLI with LITERAL arguments, no shell. The
// output travels inside the error because it is all the operator has to
// understand what THEIR tool said.
func runFirewallCmd(ctx context.Context, name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s is not in the PATH: %w", name, err)
	}
	raw, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err,
			strings.TrimSpace(string(raw)))
	}
	return nil
}
