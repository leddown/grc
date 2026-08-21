package crisisexercise

import (
	"fmt"
	"strings"
)

// This file models the part of a crisis that a discussion cannot: the clocks.
//
// A crisis exercise that narrates its regulatory obligations produces a warm
// feeling and no evidence. Modelling them produces the one artefact a
// supervisor actually asks for — which notification was due when, whether it
// went, and how late. The arithmetic below is what turns "we discussed
// reporting" into "the initial notification was due at T+3h50 and was sent at
// T+6h15, because classification happened ninety minutes after the facts
// supported it".

// Clock regimes.
const (
	RegimeDORAInitial      = "dora_initial"
	RegimeDORAIntermediate = "dora_intermediate"
	RegimeDORAFinal        = "dora_final"
	RegimeManagementBody   = "management_body"
	RegimeECBSSM           = "ecb_ssm"
	RegimeNIS2Early        = "nis2_early_warning"
	RegimeNIS2Notification = "nis2_notification"
	RegimeNIS2Final        = "nis2_final"
	RegimeGDPRAuthority    = "gdpr_authority"
	RegimeGDPRSubjects     = "gdpr_subjects"
	RegimeSchemeNotice     = "scheme_notice"
	RegimeCustomerNotice   = "customer_notice"
)

// Common durations, in minutes, so the arithmetic below reads as the regulation
// does.
const (
	oneHour     = 60
	fourHours   = 4 * 60
	twentyFour  = 24 * 60
	seventyTwo  = 72 * 60
	oneMonthMin = 30 * 24 * 60
)

// ---- classification ----

// Evaluate applies the DORA major-incident rule to a filled-in classification
// and returns it with Major and Rationale set.
//
// The rule, from Commission Delegated Regulation (EU) 2024/1772: an incident is
// major where critical services are affected *and* either the data-losses
// criterion is met, or two or more of the remaining criteria are.
//
// What this deliberately does not do is decide whether a criterion is met. The
// real thresholds are numeric, sector-specific and revised; encoding a number
// here would produce an authoritative-looking answer that is wrong for some
// entities and out of date for the rest. So the team assesses each criterion
// against its own thresholds — which is the judgement the exercise is meant to
// rehearse — and this applies the combination rule, which is the part teams
// reliably get wrong on a whiteboard.
func Evaluate(c Classification) Classification {
	if !c.CriticalServicesAffected {
		c.Major = false
		c.Rationale = "Critical or important services were not affected, so the incident does not meet the basic condition for a major incident under DORA Article 18. The remaining criteria are not reached."
		return c
	}

	var met []string
	if c.ClientsMaterial {
		met = append(met, "clients and financial counterparts affected")
	}
	if c.TransactionsMaterial {
		met = append(met, "transactions affected")
	}
	if c.ReputationalMaterial {
		met = append(met, "reputational impact")
	}
	if c.DurationMaterial {
		met = append(met, "duration and service downtime")
	}
	if c.GeographicalMaterial {
		met = append(met, "geographical spread")
	}
	if c.EconomicMaterial {
		met = append(met, "economic impact")
	}

	switch {
	case c.DataLossesMaterial:
		c.Major = true
		c.Rationale = "Critical or important services were affected and the data-losses criterion is met, which is sufficient on its own."
		if len(met) > 0 {
			c.Rationale += " Also met: " + strings.Join(met, ", ") + "."
		}
	case len(met) >= 2:
		c.Major = true
		c.Rationale = fmt.Sprintf("Critical or important services were affected and %d of the remaining criteria are met (%s), which is at or above the threshold of two.",
			len(met), strings.Join(met, ", "))
	case len(met) == 1:
		c.Major = false
		c.Rationale = fmt.Sprintf("Critical or important services were affected but only one criterion is met (%s). Two are required where the data-losses criterion is not. This is the classification most worth revisiting as facts develop — a single criterion is one new fact away from being two.", met[0])
	default:
		c.Major = false
		c.Rationale = "Critical or important services were affected but no materiality criterion is met on the information assessed."
	}
	return c
}

// ---- clocks ----

// ComputeClocks derives the notification obligations the scenario has started,
// from the exercise's perimeter and the team's classification.
//
// Offsets are T+ minutes from exercise start, so the same exercise can be run
// on any date and the clocks still line up with the injects. A clock that does
// not apply is returned anyway, marked not-applicable: an obligation the team
// correctly ruled out is worth showing, because the alternative is a report
// that cannot distinguish "we considered GDPR and it did not apply" from "we
// never thought about GDPR".
func ComputeClocks(ex Exercise, c Classification) []Clock {
	j := JurisdictionByKey(ex.Jurisdiction)
	c = Evaluate(c)

	// The DORA initial notification is due at the earlier of four hours from
	// classification and twenty-four hours from awareness. Teams read the four
	// hours as a budget and the twenty-four as the real deadline; it is the
	// other way round, and the whichever-is-earlier arithmetic here is what
	// makes that visible.
	initialDue := c.ClassifiedOffset + fourHours
	initialBasis := "Four hours from classification as major."
	if cap24 := c.AwareOffset + twentyFour; cap24 < initialDue {
		initialDue = cap24
		initialBasis = "Twenty-four hours from becoming aware — the outer cap, which here falls before the four-hour clock from classification does. Classifying later does not move this."
	}

	intermediateDue := initialDue + seventyTwo
	finalDue := intermediateDue + oneMonthMin

	clocks := []Clock{
		{
			Regime:    RegimeManagementBody,
			Authority: "Management body / senior management (internal)",
			Label:     "Escalate to senior management and the management body",
			Basis: "DORA Article 17 requires escalation procedures to senior management and the management body as part of the incident management process. " +
				"The timing is the entity's own; an hour from classification is a defensible working target and the point is that there is one.",
			DueOffset:    c.ClassifiedOffset + oneHour,
			ActualOffset: -1,
			Status:       ClockPending,
			Source:       "dora-art-17",
		},
		{
			Regime:       RegimeDORAInitial,
			Authority:    j.CompetentAuthority,
			Label:        "DORA initial notification of a major ICT-related incident",
			Basis:        initialBasis,
			DueOffset:    initialDue,
			ActualOffset: -1,
			Status:       ClockPending,
			Source:       "dora-art-19",
		},
		{
			Regime:       RegimeDORAIntermediate,
			Authority:    j.CompetentAuthority,
			Label:        "DORA intermediate report",
			Basis:        "Seventy-two hours from the initial notification, and due even where nothing has changed — a team that waits for progress misses it.",
			DueOffset:    intermediateDue,
			ActualOffset: -1,
			Status:       ClockPending,
			Source:       "dora-art-19",
		},
		{
			Regime:       RegimeDORAFinal,
			Authority:    j.CompetentAuthority,
			Label:        "DORA final report",
			Basis:        "No later than one month after the latest intermediate report, including root cause analysis and remediation.",
			DueOffset:    finalDue,
			ActualOffset: -1,
			Status:       ClockPending,
			Source:       "dora-art-19",
		},
	}

	if !c.Major {
		// The DORA clocks are the ones that turn off when the incident is not
		// major. The internal escalation does not: an incident serious enough
		// to classify is serious enough to tell the management body about.
		for i := range clocks {
			if strings.HasPrefix(clocks[i].Regime, "dora_") {
				clocks[i].Status = ClockNotApplicable
				clocks[i].Notes = "The incident was not classified as major, so the Article 19 reporting obligation is not engaged. Revisit if the assessment changes — reclassification restarts these clocks from the new classification time."
			}
		}
	}

	// ECB reporting for a significant institution runs alongside the DORA
	// obligation rather than inside it, and its timing is set by the Joint
	// Supervisory Team. Assuming it mirrors Article 19 is the mistake this
	// entry exists to prevent.
	ecb := Clock{
		Regime:       RegimeECBSSM,
		Authority:    "European Central Bank, via the Joint Supervisory Team",
		Label:        "ECB Banking Supervision cyber incident notification",
		Basis:        "Set by the ECB's own cyber incident reporting expectation for significant institutions, separately from DORA Article 19.",
		DueOffset:    c.ClassifiedOffset + fourHours,
		ActualOffset: -1,
		Status:       ClockPending,
		Source:       "ecb-ssm",
		Notes: "The due time shown is a working placeholder aligned to the DORA initial notification. Confirm the current expectation with " +
			"the Joint Supervisory Team before running the exercise and correct it here — a rehearsed deadline that turns out to be " +
			"wrong is worse than none.",
	}
	if !strings.EqualFold(strings.TrimSpace(ex.Supervision), "significant") {
		ecb.Status = ClockNotApplicable
		ecb.Notes = "The entity is not a significant institution under direct ECB supervision, so this channel does not apply. It does apply to the group parent in several Baltic banking groups — check where the exercise sits in the group."
	}
	clocks = append(clocks, ecb)

	nis2Applicable := c.NIS2Significant
	nis2Note := ""
	if !nis2Applicable {
		nis2Note = "Financial entities are largely reported under DORA rather than under the national NIS2 transposition. This clock still matters where the " +
			"entity has non-financial group companies, where it is itself a provider to others, or where the national CSIRT has asked to be told regardless — " +
			"which in the Baltic states it often has."
	}
	clocks = append(clocks,
		nis2Clock(RegimeNIS2Early, j, "NIS2 early warning to the national CSIRT",
			"Twenty-four hours from becoming aware of a significant incident.",
			c.AwareOffset+twentyFour, nis2Applicable, nis2Note),
		nis2Clock(RegimeNIS2Notification, j, "NIS2 incident notification",
			"Seventy-two hours from becoming aware, updating the early warning with an initial assessment.",
			c.AwareOffset+seventyTwo, nis2Applicable, nis2Note),
		nis2Clock(RegimeNIS2Final, j, "NIS2 final report",
			"One month after the incident notification.",
			c.AwareOffset+seventyTwo+oneMonthMin, nis2Applicable, nis2Note),
	)

	gdprAuthority := Clock{
		Regime:       RegimeGDPRAuthority,
		Authority:    j.DPA,
		Label:        "GDPR Article 33 notification of a personal data breach",
		Basis:        "Without undue delay and, where feasible, within 72 hours of becoming aware of the personal data breach. Awareness of the breach is a different moment from awareness of the incident, and it is usually later.",
		DueOffset:    c.AwareOffset + seventyTwo,
		ActualOffset: -1,
		Status:       ClockPending,
		Source:       "gdpr-art-33",
	}
	gdprSubjects := Clock{
		Regime:       RegimeGDPRSubjects,
		Authority:    "Affected data subjects",
		Label:        "GDPR Article 34 communication to data subjects",
		Basis:        "Without undue delay where the breach is likely to result in a high risk to individuals. There is no fixed deadline, which is why it slips.",
		DueOffset:    c.AwareOffset + seventyTwo,
		ActualOffset: -1,
		Status:       ClockPending,
		Source:       "gdpr-art-34",
		Notes:        "The due time shown is a working target, not a legal deadline. The obligation is 'without undue delay', which a supervisor will assess against what the entity knew and when.",
	}
	if !c.PersonalDataBreach {
		for _, clk := range []*Clock{&gdprAuthority, &gdprSubjects} {
			clk.Status = ClockNotApplicable
			clk.Notes = "No personal data breach was assessed. Record the basis for that conclusion — 'the attacker only encrypted, they did not exfiltrate' is an assumption until someone has evidence for it."
		}
	}
	clocks = append(clocks, gdprAuthority, gdprSubjects)

	clocks = append(clocks,
		Clock{
			Regime:       RegimeSchemeNotice,
			Authority:    "Payment schemes, market infrastructures and correspondent banks",
			Label:        "Scheme and market infrastructure notification",
			Basis:        "Contractual rather than regulatory: card scheme rulebooks, TARGET and CSD participation agreements each carry their own notification trigger and window, typically shorter than the regulatory ones.",
			DueOffset:    c.ClassifiedOffset + 2*oneHour,
			ActualOffset: -1,
			Status:       ClockPending,
			Source:       "cpmi-iosco",
			Notes:        "Check the actual windows in your scheme rulebooks and correct this entry. This is the obligation most often discovered during a real incident rather than before one.",
		},
		Clock{
			Regime:       RegimeCustomerNotice,
			Authority:    "Customers and the public",
			Label:        "Customer and public communication",
			Basis:        "DORA Article 14 requires a crisis communication plan for responsible disclosure of major incidents to clients, counterparts and the public. The timing is the entity's, and so is the consequence of getting it wrong.",
			DueOffset:    c.ClassifiedOffset + fourHours,
			ActualOffset: -1,
			Status:       ClockPending,
			Source:       "dora-art-14",
			Notes:        "In practice this is set by the market, not the plan: the deadline is the moment a customer or a journalist says it first. Judge the team against that, and note which languages the statement existed in when it went out — " + strings.Join(j.Languages, ", ") + ".",
		},
	)

	for i := range clocks {
		clocks[i].Ordinal = i + 1
	}
	return clocks
}

func nis2Clock(regime string, j Jurisdiction, label, basis string, due int, applicable bool, note string) Clock {
	c := Clock{
		Regime:       regime,
		Authority:    j.CSIRT,
		Label:        label,
		Basis:        basis,
		DueOffset:    due,
		ActualOffset: -1,
		Status:       ClockPending,
		Source:       "nis2-art-23",
	}
	if !applicable {
		c.Status = ClockNotApplicable
		c.Notes = note
	}
	return c
}

// ScoreClock sets a clock's status from the offset at which the notification
// actually went. A negative actual means it never did, which is a miss rather
// than a pending item once the exercise has ended.
func ScoreClock(c Clock, actualOffset int, exerciseEnded bool) Clock {
	if c.Status == ClockNotApplicable {
		return c
	}
	c.ActualOffset = actualOffset
	switch {
	case actualOffset < 0 && exerciseEnded:
		c.Status = ClockMissed
	case actualOffset < 0:
		c.Status = ClockPending
	case actualOffset <= c.DueOffset:
		c.Status = ClockMet
	default:
		c.Status = ClockMissed
	}
	return c
}

// LatenessMinutes reports how late a notification was, or 0 when it was on
// time or has not happened.
func (c Clock) LatenessMinutes() int {
	if c.ActualOffset < 0 || c.ActualOffset <= c.DueOffset {
		return 0
	}
	return c.ActualOffset - c.DueOffset
}

// FormatOffset renders T+ minutes the way an exercise log reads them.
func FormatOffset(minutes int) string {
	if minutes < 0 {
		return "—"
	}
	days := minutes / (24 * 60)
	rem := minutes % (24 * 60)
	hours := rem / 60
	mins := rem % 60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("T+%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("T+%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("T+%dh %02dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("T+%dh", hours)
	default:
		return fmt.Sprintf("T+%dm", mins)
	}
}
