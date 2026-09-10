package pageui

import (
	"html"
	"strings"
)

// NavItem is a single destination in the global sidebar.
type NavItem struct {
	Path  string
	Label string
}

// NavGroup is a labeled section of the sidebar. Order here is render order.
type NavGroup struct {
	Label string
	Items []NavItem
}

// homeItem is pinned above every group rather than living inside one.
var homeItem = NavItem{Path: "/", Label: "Home"}

// navGroups is the single source of truth for every page's navigation. Every
// page renders the same groups via Nav — there used to be four separate tab
// lists (primary/home/risk-register/CRM) that had each drifted to cover a
// different subset of routes, so a page using the "primary" list could not
// link to Risk Register, CRM, or API Docs at all without detouring through
// Home first. One shared, grouped list removes that gap by construction.
var navGroups = []NavGroup{
	{
		Label: "Catalog",
		Items: []NavItem{
			{Path: "/controls", Label: "Controls"},
			{Path: "/controls/manage", Label: "Control Editor"},
			{Path: "/controls/hierarchy", Label: "Hierarchy"},
			{Path: "/controls/family-visibility", Label: "Family Filters"},
			{Path: "/security-nfrs", Label: "Security NFRs"},
			{Path: "/security-nfrs/manage", Label: "Security NFR Editor"},
			{Path: "/security-nfrs/links", Label: "NFR-Control Links"},
			{Path: "/nfr-enrichment", Label: "NFR Enrichment"},
			{Path: "/asset-types", Label: "Asset Types"},
		},
	},
	{
		Label: "Compliance & Risk",
		Items: []NavItem{
			{Path: "/risk-register", Label: "Risk Register"},
			{Path: "/risk-register/manage", Label: "Risk Register Editor"},
			{Path: "/exceptions", Label: "Exceptions"},
			{Path: "/policies", Label: "Policy Library"},
			{Path: "/policies/manage", Label: "Policy Editor"},
			{Path: "/policies/coverage", Label: "Policy Coverage"},
			{Path: "/regulation-coverage", Label: "Regulation Coverage"},
			{Path: "/wiz-rules", Label: "Wiz Rules"},
		},
	},
	{
		// Its own section, like AI Chat, so the topbar renders it as a direct
		// link: an exercise runs across its own design, delivery and reporting
		// tabs, and it was one entry buried among eight compliance pages.
		Label: "Crisis Exercises",
		Items: []NavItem{
			{Path: "/crisis-exercises", Label: "Crisis Exercises"},
		},
	},
	{
		Label: "Reporting & Data",
		Items: []NavItem{
			{Path: "/reports", Label: "Reports"},
			{Path: "/JSON_view", Label: "JSON_view"},
			{Path: "/jira/json", Label: "Jira JSON"},
			{Path: "/jira/reports", Label: "Jira Reports"},
			{Path: "/changelog", Label: "Change Log"},
		},
	},
	{
		// A section of its own rather than one entry among the reports: it is
		// the page most often reached from the middle of other work, and a
		// single-page section is rendered as a link in the topbar, so it is one
		// click from anywhere instead of a section switch and then a tab.
		Label: "AI Chat",
		Items: []NavItem{
			{Path: "/ai-chat", Label: "AI Chat"},
		},
	},
	{
		// What is left of the old Company tab after the company profile moved to
		// wintermute: the policy-document renderer, which belongs beside the
		// policy module it renders rather than under a heading about the firm.
		Label: "Documents",
		Items: []NavItem{
			{Path: "/templates", Label: "Templates"},
			{Path: "/templates/manage", Label: "Template Brand"},
		},
	},
}

// adminGroup renders last and gets a visually deprioritized style (see the
// .tab-group-admin rule in theme_middleware.go) — these are operational
// pages, not day-to-day work, and best-practice sidebar guidance is to keep
// that kind of item from competing with primary navigation for attention.
var adminGroup = NavGroup{
	Label: "Admin",
	Items: []NavItem{
		// First in the group because it is the one page here that is for
		// everyone rather than for whoever runs the installation.
		{Path: "/help", Label: "Help"},
		{Path: "/settings", Label: "Settings"},
		{Path: "/docs", Label: "API Docs"},
		{Path: "/utilities", Label: "Utilities"},
	},
}

// Nav renders the global sidebar navigation, identical on every page except
// for which item is marked active. activePath need not be an exact entry —
// e.g. /controls/detail/AC-1 has no entry of its own, so it highlights its
// nearest ancestor, /controls, via longest-prefix matching. That also means
// a detail/edit page reachable only by dynamic ID never needs a synthetic
// "extra" tab just to show something active while browsing it.
func Nav(activePath string) string {
	activePath = strings.TrimSpace(activePath)
	active := bestMatch(activePath)

	var b strings.Builder
	b.WriteString(`<nav class="tabs" aria-label="Primary">`)
	writeTab(&b, homeItem, active)
	for _, group := range navGroups {
		writeGroup(&b, group, active)
	}
	writeGroup(&b, adminGroup, active)
	b.WriteString(`</nav>`)
	return b.String()
}

// bestMatch finds the nav item whose path is the longest prefix of
// activePath, matching either exactly or up to a following "/" so
// /controls does not falsely match a hypothetical /controls-archive.
// Home ("/") only ever matches activePath == "/", since every path is
// technically prefixed by "/" and that would otherwise always win as the
// shortest, never-losing candidate.
func bestMatch(activePath string) string {
	best := ""
	consider := func(path string) {
		if path == "/" {
			if activePath == "/" && len(best) == 0 {
				best = "/"
			}
			return
		}
		if activePath == path || strings.HasPrefix(activePath, path+"/") {
			if len(path) > len(best) {
				best = path
			}
		}
	}

	consider(homeItem.Path)
	for _, group := range navGroups {
		for _, item := range group.Items {
			consider(item.Path)
		}
	}
	for _, item := range adminGroup.Items {
		consider(item.Path)
	}
	return best
}

func writeGroup(b *strings.Builder, group NavGroup, activePathMatch string) {
	className := "tab-group"
	if group.Label == adminGroup.Label {
		className = "tab-group tab-group-admin"
	}
	b.WriteString(`<div class="`)
	b.WriteString(className)
	b.WriteString(`">`)
	b.WriteString(`<p class="tab-group-label">`)
	b.WriteString(html.EscapeString(group.Label))
	b.WriteString(`</p>`)
	for _, item := range group.Items {
		writeTab(b, item, activePathMatch)
	}
	b.WriteString(`</div>`)
}

func writeTab(b *strings.Builder, item NavItem, activePathMatch string) {
	path := strings.TrimSpace(item.Path)
	label := strings.TrimSpace(item.Label)
	if path == "" || label == "" {
		return
	}

	className := "tab"
	if path == activePathMatch {
		className = "tab active"
	}

	b.WriteString(`<a class="`)
	b.WriteString(className)
	b.WriteString(`" href="`)
	b.WriteString(html.EscapeString(path))
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(label))
	b.WriteString(`</a>`)
}
