package prow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
)

const (
	labelLGTM     = "lgtm"
	labelApproved = "approved"
	labelHold     = "do-not-merge/hold"

	// approvalNotifierMarker starts the comment where Prow's approve plugin
	// summarizes who approved the PR and what still needs approving
	approvalNotifierMarker = "[APPROVALNOTIFIER]"
)

// approval is what Prow's approval notifier comment says.
type approval struct {
	approved bool
	// approvers approved the PR, in the order listed
	approvers []approver
	// pending are the OWNERS files still needing an approver's approval
	pending []string
	// satisfied are the OWNERS files approved, with who approved them
	satisfied []ownersApproval
	// suggested are the approvers Prow suggests assigning
	suggested []string
}

type approver struct {
	login string
	// how is how they approved, e.g. "Author self-approved"
	how string
}

type ownersApproval struct {
	path string
	by   []string
}

var (
	approvedByRegex  = regexp.MustCompile(`(?m)^This pull-request has been approved by:(.*)$`)
	htmlTagRegex     = regexp.MustCompile(`<[^>]*>`)
	titleAttrRegex   = regexp.MustCompile(`title="([^"]*)"`)
	pendingRegex     = regexp.MustCompile(`(?m)^\s*- \*\*\[([^\]]+)\]\([^)]*\)\*\*`)
	satisfiedRegex   = regexp.MustCompile(`(?m)^\s*- ~~\[([^\]]+)\]\([^)]*\)~~(?: \[([^\]]*)\])?`)
	metaRegex        = regexp.MustCompile(`<!-- META=(\{.*?\}) -->`)
	lgtmCommandRegex = regexp.MustCompile(`(?m)^/lgtm(?:[ \t]+(cancel))?[ \t]*\r?$`)
)

// parseApproval parses Prow's approval notifier comment.
func parseApproval(body string) approval {
	a := approval{
		approved: strings.Contains(body, "This PR is **APPROVED**"),
	}

	if m := approvedByRegex.FindStringSubmatch(body); m != nil {
		for _, part := range strings.Split(m[1], ",") {
			how := ""
			if t := titleAttrRegex.FindStringSubmatch(part); t != nil {
				how = t[1]
			}
			login := strings.Trim(htmlTagRegex.ReplaceAllString(part, ""), " *")
			if login != "" {
				a.approvers = append(a.approvers, approver{login: login, how: how})
			}
		}
	}

	for _, m := range pendingRegex.FindAllStringSubmatch(body, -1) {
		a.pending = append(a.pending, m[1])
	}
	for _, m := range satisfiedRegex.FindAllStringSubmatch(body, -1) {
		oa := ownersApproval{path: m[1]}
		for _, login := range strings.Split(m[2], ",") {
			if login = strings.TrimSpace(login); login != "" {
				oa.by = append(oa.by, login)
			}
		}
		a.satisfied = append(a.satisfied, oa)
	}

	if m := metaRegex.FindStringSubmatch(body); m != nil {
		var meta struct {
			Approvers []string `json:"approvers"`
		}
		if json.Unmarshal([]byte(m[1]), &meta) == nil {
			a.suggested = meta.Approvers
		}
	}
	return a
}

// latestApproval returns what Prow's latest approval notifier comment on the
// PR says, if it commented one.
func latestApproval(pr plugins.PR) (approval, bool) {
	if !pr.IsEnriched || pr.Enriched == nil {
		return approval{}, false
	}
	var body string
	var at time.Time
	for _, c := range pr.Enriched.Comments.Nodes {
		if strings.HasPrefix(strings.TrimSpace(c.Body), approvalNotifierMarker) &&
			(body == "" || c.UpdatedAt.After(at)) {
			body, at = c.Body, c.UpdatedAt
		}
	}
	if body == "" {
		return approval{}, false
	}
	return parseApproval(body), true
}

// lgtmGivers returns who said /lgtm in the PR's comments and reviews, and
// didn't cancel it since, in the order they said it.
func lgtmGivers(pr plugins.PR) []string {
	if !pr.IsEnriched || pr.Enriched == nil {
		return nil
	}
	type said struct {
		login  string
		cancel bool
		at     time.Time
	}
	var all []said
	collect := func(login, body string, at time.Time) {
		for _, m := range lgtmCommandRegex.FindAllStringSubmatch(body, -1) {
			all = append(all, said{login: login, cancel: m[1] != "", at: at})
		}
	}
	for _, c := range pr.Enriched.Comments.Nodes {
		collect(c.Author.Login, c.Body, c.CreatedAt)
	}
	for _, r := range pr.Enriched.Reviews.Nodes {
		collect(r.Author.Login, r.Body, r.CreatedAt)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })

	var givers []string
	for _, s := range all {
		i := -1
		for j, g := range givers {
			if g == s.login {
				i = j
			}
		}
		switch {
		case s.cancel && i >= 0:
			givers = append(givers[:i], givers[i+1:]...)
		case !s.cancel && i < 0:
			givers = append(givers, s.login)
		}
	}
	return givers
}

// Panels shows where the PR stands with Prow: whether it has the lgtm and
// approved labels, who approved it, which OWNERS files still need approving
// and what else blocks merging it.
func (p *Plugin) Panels(pr plugins.PR) []plugins.Panel {
	if !pr.IsEnriched || !managesPR(pr) {
		return nil
	}
	a, hasApproval := latestApproval(pr)
	lgtm := pr.HasLabel(labelLGTM)
	approved := pr.HasLabel(labelApproved) || hasApproval && a.approved

	var rows []plugins.PanelRow

	// LGTM
	givers := lgtmGivers(pr)
	switch {
	case lgtm && len(givers) > 0:
		rows = append(rows, plugins.PanelRow{Status: plugins.StatusSuccess,
			Text: "LGTM by " + mentions(givers)})
	case lgtm:
		rows = append(rows, plugins.PanelRow{Status: plugins.StatusSuccess, Text: "LGTM"})
	default:
		rows = append(rows, plugins.PanelRow{Status: plugins.StatusPending,
			Text: "Needs /lgtm from a reviewer"})
	}

	// Approval
	if hasApproval && len(a.approvers) > 0 {
		names := make([]string, 0, len(a.approvers))
		for _, ap := range a.approvers {
			name := "@" + ap.login
			if strings.Contains(strings.ToLower(ap.how), "self-approved") {
				name += " (author)"
			}
			names = append(names, name)
		}
		status := plugins.StatusSuccess
		if !approved {
			// Some of them only approved some of the files
			status = plugins.StatusNeutral
		}
		rows = append(rows, plugins.PanelRow{Status: status,
			Text: "Approved by " + strings.Join(names, ", ")})
	} else if approved {
		rows = append(rows, plugins.PanelRow{Status: plugins.StatusSuccess, Text: "Approved"})
	}
	if !approved {
		switch {
		case hasApproval && len(a.pending) > 0:
			rows = append(rows, plugins.PanelRow{Status: plugins.StatusPending,
				Text: "Needs /approve for " + strings.Join(a.pending, ", ")})
		default:
			rows = append(rows, plugins.PanelRow{Status: plugins.StatusPending,
				Text: "Needs /approve from an approver"})
		}
		if len(a.suggested) > 0 {
			rows = append(rows, plugins.PanelRow{Faint: true, Glyph: " ",
				Text: "Suggested approvers: " + mentions(a.suggested)})
		}
	}
	for _, s := range a.satisfied {
		text := s.path
		if len(s.by) > 0 {
			text += " approved by " + mentions(s.by)
		}
		rows = append(rows, plugins.PanelRow{Faint: true, Glyph: " ", Text: text})
	}

	// Other things blocking merging
	blocked := false
	for _, l := range pr.Labels() {
		switch {
		case l == labelHold:
			blocked = true
			rows = append(rows, plugins.PanelRow{Status: plugins.StatusFailure,
				Text: "On hold · /hold cancel to release"})
		case strings.HasPrefix(l, "do-not-merge/"), l == "needs-rebase", l == "needs-ok-to-test":
			blocked = true
			rows = append(rows, plugins.PanelRow{Status: plugins.StatusFailure, Text: l})
		}
	}

	panel := plugins.Panel{Title: "Prow", Rows: rows}
	switch {
	case blocked:
		panel.Status = plugins.StatusFailure
		panel.Subtitle = "Merging is blocked"
	case lgtm && approved:
		panel.Status = plugins.StatusSuccess
		panel.Subtitle = "Approved and LGTM'd, Tide merges it once checks pass"
	case approved:
		panel.Status = plugins.StatusPending
		panel.Subtitle = "Approved, waiting on lgtm"
	case lgtm:
		panel.Status = plugins.StatusPending
		panel.Subtitle = "LGTM'd, waiting on approval"
	default:
		panel.Status = plugins.StatusPending
		panel.Subtitle = "Waiting on lgtm and approval"
	}
	return []plugins.Panel{panel}
}

func mentions(logins []string) string {
	out := make([]string, len(logins))
	for i, l := range logins {
		out[i] = fmt.Sprintf("@%s", l)
	}
	return strings.Join(out, ", ")
}
