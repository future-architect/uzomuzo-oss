package analysis

import "time"

// PackagePublishStatus describes whether dated package records name an account.
type PackagePublishStatus string

const (
	// PackagePublishFound means a dated version names the account as publisher.
	PackagePublishFound PackagePublishStatus = "found"
	// PackagePublishNotFound means no version present in versions has both a
	// publish time in time and an _npmUser naming this account. Removed or
	// unattributed versions are not evidence that the account never published.
	PackagePublishNotFound PackagePublishStatus = "not_found"
)

// PackagePublish is the latest dated publish by an account in one package.
type PackagePublish struct {
	// Status is found when a dated version names the account; otherwise it is not_found.
	Status PackagePublishStatus
	// LastPublishedAt is the latest dated publish by the account in this package.
	// It is zero when Status is not_found.
	LastPublishedAt time.Time
	// Version is the lowest string-ordered version published at LastPublishedAt.
	// It is empty when Status is not_found.
	Version string
}

// PackagePublishFor reads only the supplied package's visible publish history.
func PackagePublishFor(account string, h PublisherPackageHistory) *PackagePublish {
	result := &PackagePublish{Status: PackagePublishNotFound}
	if account == "" || len(h.Latest[account]) == 0 {
		return result
	}
	latest := h.Latest[account]
	result.Status = PackagePublishFound
	result.LastPublishedAt = latest[0].PublishedAt
	result.Version = latest[0].Version
	for _, publish := range latest[1:] {
		if publish.Version < result.Version {
			result.Version = publish.Version
		}
	}
	return result
}

// VersionPublisherKind describes the analysed version's named publisher.
type VersionPublisherKind string

const (
	// VersionPublisherTrustedPublishing is the exact npm GitHub Actions identity.
	VersionPublisherTrustedPublishing VersionPublisherKind = "trusted_publishing"
	// VersionPublisherOther is any other named publisher; it does not imply a manual publish.
	VersionPublisherOther VersionPublisherKind = "other"
	// VersionPublisherUnknown means the version has no named publisher record.
	VersionPublisherUnknown VersionPublisherKind = "unknown"
	// TrustedPublishingPublisher is npm's GitHub Actions publisher identity.
	TrustedPublishingPublisher = "GitHub Actions"
)

// VersionPublisherKindFor classifies the exact analysed version in a release history.
func VersionPublisherKindFor(h *ReleaseHistory, version string) VersionPublisherKind {
	if h == nil || version == "" {
		return VersionPublisherUnknown
	}
	name := h.Publishers[version]
	if name == "" {
		return VersionPublisherUnknown
	}
	if name == TrustedPublishingPublisher {
		return VersionPublisherTrustedPublishing
	}
	return VersionPublisherOther
}
