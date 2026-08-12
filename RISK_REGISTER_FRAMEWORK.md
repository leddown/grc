# Security Risk Register Framework

This project now includes a dedicated security risk register endpoint and editor:

- Page: `/risk-register`
- Editor: `/risk-register/manage`
- Data API: `/risk-register/data`
- Admin write APIs: `POST /risk-register`, `PUT /risk-register/:id`, `DELETE /risk-register/:id`

## Research basis

The design is based on NIST guidance that emphasizes:

- Scenario-based risk identification and estimation
- Explicit likelihood/impact estimation
- Residual risk tracking after controls
- Response decision and action tracking
- Ongoing monitoring and review
- Integration of cybersecurity risk with enterprise risk communication

### Sources reviewed

- NIST SP 800-30 Rev.1, Guide for Conducting Risk Assessments  
  https://csrc.nist.gov/pubs/sp/800/30/r1/final
- NIST SP 800-37 Rev.2, Risk Management Framework (continuous monitoring and risk response)  
  https://csrc.nist.gov/pubs/sp/800/37/r2/final
- NISTIR 8286, Integrating Cybersecurity and Enterprise Risk Management (ERM)  
  https://csrc.nist.gov/Pubs/ir/8286/Final
- NISTIR 8286A Rev.1, Identifying and Estimating Cybersecurity Risk for ERM  
  https://csrc.nist.gov/pubs/ir/8286/a/r1/final
- NISTIR 8286 supplemental schemas (risk register + risk detail record)  
  https://csrc.nist.gov/Pubs/ir/8286/Final

## Register structure used in this app

Each risk record tracks:

- Identity/context: `risk_id`, `title`, `business_unit`, `asset`
- Scenario details: `threat_source`, `vulnerability`
- Inherent risk: `likelihood`, `impact`, computed `inherent_score`
- Existing safeguards: `current_controls`
- Residual risk: `residual_likelihood`, `residual_impact`, computed `residual_score`
- Response plan: `response_strategy` (e.g. mitigate/accept/transfer/avoid), `response_action`
- Accountability/governance: `owner`, `status`, `risk_appetite_aligned`
- Time tracking: `target_date`, `last_review_date`, `next_review_date`
- Notes/evidence: `notes`

## Scoring model

- Likelihood and impact are normalized to `1..5`.
- Inherent score is calculated as `likelihood * impact`.
- Residual score is calculated as `residual_likelihood * residual_impact`.

This makes prioritization operationally simple while retaining explicit residual risk tracking.

## Best-practice operating model

1. Capture risk as a scenario, not only a label.
2. Record both inherent and residual risk so control effectiveness is visible.
3. Require an owner and explicit response strategy per item.
4. Set target and review dates for ongoing monitoring.
5. Keep status values explicit (`Open`, `In Progress`, `Accepted`, `Closed`).
6. Sort and triage by residual risk first, then inherent risk.
7. Periodically reconcile risk response with enterprise risk appetite/tolerance.

## Security and authorization in this app

- Reads are available at `/risk-register/data`.
- Write operations are admin-protected through existing auth middleware:
  - `POST /risk-register`
  - `PUT /risk-register/:id`
  - `DELETE /risk-register/:id`

## Notes

- This implementation is intentionally compact and can be extended with:
  - linked control IDs,
  - quantitative loss estimates,
  - workflow approvals,
  - evidence attachments,
  - and risk-to-project/program rollups.
