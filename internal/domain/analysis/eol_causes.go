package analysis

// EOLCause describes the cause (authoritative trigger or inferred condition) that produced an EOL label (Confirmed/Effective/Planned).
type EOLCause string

// Cause constants (simple, self-explanatory terms).
const (
	// Confirmed / authoritative origins
	EOLCausePrimarySource      EOLCause = "primary_source"
	EOLCauseCatalogMatch       EOLCause = "catalog_match"
	EOLCauseArchived           EOLCause = "archived"
	EOLCausePackagistAbandoned EOLCause = "packagist_abandoned"
	EOLCauseNuGetDeprecated    EOLCause = "nuget_deprecated"
	EOLCauseNpmDeprecated      EOLCause = "npm_deprecated"
	EOLCauseMavenRelocated     EOLCause = "maven_relocated"
	EOLCausePlanned            EOLCause = "planned"

	// Effective / inferred origins
	EOLCauseMissingScorecardOpenVulns EOLCause = "missing_scorecard_open_vulns"
	EOLCausePartialScorecardOpenVulns EOLCause = "partial_scorecard_open_vulns"
	EOLCauseLowMaintInactiveVulns     EOLCause = "low_maint_inactive_vulns"
	EOLCauseInactiveVulns             EOLCause = "inactive_vulns"
	EOLCauseInactiveOnly              EOLCause = "inactive_only"
)
