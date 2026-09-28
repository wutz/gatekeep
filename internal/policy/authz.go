package policy

// Outcome is what should happen to a classified request.
type Outcome string

const (
	Allow        Outcome = "allow"         // execute immediately
	NeedApproval Outcome = "need_approval" // queue for a human approver
	Deny         Outcome = "deny"          // refuse outright
)

// Actor is the minimal identity needed for authorization decisions.
type Actor struct {
	Name  string
	Agent bool
	Role  string // viewer / operator / admin (humans only)
}

// directMax is the highest level each human role may run without approval.
var directMax = map[string]Level{
	"viewer":   L0Read,
	"operator": L1Low,
	"admin":    L2High,
}

// approveMax is the highest level each human role may approve.
var approveMax = map[string]Level{
	"viewer":   -1,
	"operator": L2High,
	"admin":    L3Critical,
}

// Authorize decides what happens when actor submits a command at level.
//
//   - Agents: L0 runs directly; anything higher needs human approval.
//   - Humans: run directly up to their role's limit; above it (and always for
//     L3) a second, different human must approve. Viewers cannot submit
//     changes at all.
func Authorize(a Actor, level Level) Outcome {
	if a.Agent {
		if level == L0Read {
			return Allow
		}
		return NeedApproval
	}
	max, ok := directMax[a.Role]
	if !ok {
		return Deny
	}
	if level <= max {
		return Allow
	}
	if a.Role == "viewer" {
		return Deny
	}
	return NeedApproval
}

// CanApprove reports whether approver may approve a request at level that
// was submitted by requester. Nobody may approve their own request.
func CanApprove(approver Actor, requester string, level Level) bool {
	if approver.Agent || approver.Name == requester {
		return false
	}
	max, ok := approveMax[approver.Role]
	return ok && level <= max
}
