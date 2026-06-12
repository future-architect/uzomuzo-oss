// Package diet orchestrates the 4-phase dependency diet analysis pipeline.
package diet

import (
	"log/slog"
	"strings"

	"github.com/package-url/packageurl-go"
)

// isWorkspaceDep returns true if the PURL represents a local workspace package
// (npm/yarn/pnpm monorepo internal) that should be excluded from diet analysis.
func isWorkspaceDep(purlStr string) bool {
	parsed, err := packageurl.FromString(purlStr)
	if err != nil {
		return false
	}
	if parsed.Type != "npm" {
		return false
	}
	v := parsed.Version
	return v == "0.0.0-use.local" ||
		strings.HasPrefix(v, "workspace:") ||
		strings.HasPrefix(v, "link:") ||
		strings.HasPrefix(v, "file:")
}

// filterWorkspaceDeps removes local workspace packages from the direct deps list.
func filterWorkspaceDeps(purls []string) []string {
	filtered := make([]string, 0, len(purls))
	for _, p := range purls {
		if isWorkspaceDep(p) {
			slog.Debug("skipping workspace dependency", "purl", p)
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// buildImportPaths creates a mapping from PURL to probable import paths.
// This is a best-effort mapping used for source coupling analysis.
func buildImportPaths(purls []string) map[string][]string {
	result := make(map[string][]string, len(purls))
	for _, p := range purls {
		parsed, err := packageurl.FromString(p)
		if err != nil {
			continue
		}
		var importPath string
		switch parsed.Type {
		case "golang":
			if parsed.Namespace != "" {
				importPath = parsed.Namespace + "/" + parsed.Name
			} else {
				importPath = parsed.Name
			}
		case "npm":
			if parsed.Namespace != "" {
				// packageurl-go already includes '@' in scoped namespaces (e.g. "@types")
				importPath = parsed.Namespace + "/" + parsed.Name
			} else {
				importPath = parsed.Name
			}
		case "pypi":
			if paths := buildPyPIImportPaths(parsed.Name); len(paths) > 0 {
				result[p] = paths
			}
			continue
		case "maven":
			if paths := buildMavenImportPaths(parsed); len(paths) > 0 {
				result[p] = paths
			}
			continue
		default:
			importPath = parsed.Name
		}
		if importPath != "" {
			result[p] = []string{importPath}
		}
	}
	return result
}

// pypiPrefixes lists common PyPI distribution name prefixes that are not part
// of the actual Python import module name (e.g., "python-multipart" is imported
// as "multipart").
var pypiPrefixes = []string{
	"python-",
	"py-",
}

// buildPyPIImportPaths generates candidate Python import module names for a
// PyPI distribution name. The canonical candidate (hyphen→underscore, lowered)
// is added first when it passes validation. Additional candidates are produced
// by stripping well-known prefixes (e.g., "python-", "py-"). Each candidate
// is validated against Python identifier rules before inclusion.
func buildPyPIImportPaths(name string) []string {
	seen := make(map[string]struct{})
	var paths []string

	add := func(p string) {
		if p == "" {
			return
		}
		if !isPythonDottedIdentifierSafe(p) {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}

	// 1. Canonical: replace hyphens with underscores and lowercase.
	canonical := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	add(canonical)

	lower := strings.ToLower(name)

	// 2. Strip well-known prefixes (e.g., "python-multipart" → "multipart").
	for _, prefix := range pypiPrefixes {
		if after, ok := strings.CutPrefix(lower, prefix); ok && after != "" {
			add(strings.ReplaceAll(after, "-", "_"))
		}
	}

	return paths
}

// isPythonIdentifierSafe reports whether s is a valid Python identifier.
// The first character must be an ASCII letter or underscore; subsequent
// characters may also include ASCII digits.  This filters out candidates that
// can never match a real Python import statement (e.g., names starting with a
// digit).
func isPythonIdentifierSafe(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// isPythonDottedIdentifierSafe reports whether s is a valid dot-separated
// Python module path (e.g. "zope.interface").  Each segment between dots must
// satisfy isPythonIdentifierSafe.
func isPythonDottedIdentifierSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, seg := range strings.Split(s, ".") {
		if !isPythonIdentifierSafe(seg) {
			return false
		}
	}
	return true
}

// mavenPackageOverrides maps "groupId/artifactId" to known Java package
// prefixes for libraries where the Maven groupId does not match the actual
// Java package name.  Add entries as real-world mismatches are discovered.
var mavenPackageOverrides = map[string][]string{
	"cglib/cglib":                             {"net.sf.cglib"},
	"com.google.code.gson/gson":               {"com.google.gson"},
	"commons-beanutils/commons-beanutils":     {"org.apache.commons.beanutils"},
	"commons-codec/commons-codec":             {"org.apache.commons.codec"},
	"commons-collections/commons-collections": {"org.apache.commons.collections"},
	"commons-io/commons-io":                   {"org.apache.commons.io"},
	"commons-logging/commons-logging":         {"org.apache.commons.logging"},
	"junit/junit":                             {"junit", "org.junit"},
	"log4j/log4j":                             {"org.apache.log4j"},

	// Jackson family: Maven groupId (e.g. com.fasterxml.jackson.core) does not
	// match the actual Java package name (e.g. com.fasterxml.jackson.annotation).
	"com.fasterxml.jackson.core/jackson-annotations":           {"com.fasterxml.jackson.annotation"},
	"com.fasterxml.jackson.core/jackson-databind":              {"com.fasterxml.jackson.databind"},
	"com.fasterxml.jackson.dataformat/jackson-dataformat-csv":  {"com.fasterxml.jackson.dataformat.csv"},
	"com.fasterxml.jackson.dataformat/jackson-dataformat-xml":  {"com.fasterxml.jackson.dataformat.xml"},
	"com.fasterxml.jackson.dataformat/jackson-dataformat-yaml": {"com.fasterxml.jackson.dataformat.yaml"},
	"com.fasterxml.jackson.datatype/jackson-datatype-jsr310":   {"com.fasterxml.jackson.datatype.jsr310"},
	"com.fasterxml.jackson.module/jackson-module-kotlin":       {"com.fasterxml.jackson.module.kotlin"},

	// Guava: Maven groupId is "com.google.guava" but Java packages live under
	// "com.google.common.*".
	"com.google.guava/guava": {"com.google.common"},

	// ANTLR runtime: Maven artifactId "antlr4-runtime" maps to "org.antlr.v4.*",
	// and the ST (StringTemplate) artifact maps to "org.stringtemplate.*".
	"org.antlr/antlr4-runtime": {"org.antlr.v4"},
	"org.antlr/st4":            {"org.stringtemplate"},

	// Trove4j: Maven groupId "net.sf.trove4j" but Java packages are "gnu.trove.*".
	"net.sf.trove4j/trove4j": {"gnu.trove"},

	// Scala standard library: Maven groupId "org.scala-lang" but Java/Scala
	// packages are "scala.*".
	"org.scala-lang/scala-library": {"scala"},
	"org.scala-lang/scala-reflect": {"scala.reflect"},

	// javax.inject: groupId and artifactId both equal "javax.inject", so the
	// heuristic already produces the correct candidate, but an explicit override
	// ensures stability.
	"javax.inject/javax.inject": {"javax.inject"},

	// Spring Boot starters: groupId is always "org.springframework.boot" but each
	// starter pulls in distinct Spring libraries. Without these overrides, ALL starters
	// map to "org.springframework.boot" and receive identical coupling scores (#295).
	"org.springframework.boot/spring-boot-starter":              {"org.springframework.boot"},
	"org.springframework.boot/spring-boot-starter-web":          {"org.springframework.web", "org.springframework.boot.web"},
	"org.springframework.boot/spring-boot-starter-webflux":      {"org.springframework.web.reactive"},
	"org.springframework.boot/spring-boot-starter-data-jpa":     {"org.springframework.data.jpa", "javax.persistence", "jakarta.persistence"},
	"org.springframework.boot/spring-boot-starter-data-redis":   {"org.springframework.data.redis"},
	"org.springframework.boot/spring-boot-starter-data-mongodb": {"org.springframework.data.mongodb"},
	"org.springframework.boot/spring-boot-starter-security":     {"org.springframework.security"},
	"org.springframework.boot/spring-boot-starter-test":         {"org.springframework.boot.test", "org.springframework.test"},
	"org.springframework.boot/spring-boot-starter-actuator":     {"org.springframework.boot.actuate"},
	"org.springframework.boot/spring-boot-starter-validation":   {"javax.validation", "jakarta.validation"},
	"org.springframework.boot/spring-boot-starter-thymeleaf":    {"org.thymeleaf"},
	"org.springframework.boot/spring-boot-starter-mail":         {"org.springframework.mail", "javax.mail", "jakarta.mail"},
	"org.springframework.boot/spring-boot-starter-cache":        {"org.springframework.cache"},
	"org.springframework.boot/spring-boot-starter-aop":          {"org.aspectj", "org.springframework.aop"},
	"org.springframework.boot/spring-boot-starter-batch":        {"org.springframework.batch"},
	"org.springframework.boot/spring-boot-starter-amqp":         {"org.springframework.amqp"},
	"org.springframework.boot/spring-boot-starter-websocket":    {"org.springframework.web.socket"},
	"org.springframework.boot/spring-boot-starter-jdbc":         {"org.springframework.jdbc"},
	"org.springframework.boot/spring-boot-starter-json":         {"com.fasterxml.jackson"},
	"org.springframework.boot/spring-boot-starter-logging":      {"org.slf4j", "ch.qos.logback"},

	// Spring non-starter libraries: groupId "org.springframework" but each
	// library uses a specific sub-package.
	"org.springframework/spring-core":    {"org.springframework.core", "org.springframework.util"},
	"org.springframework/spring-context": {"org.springframework.context"},
	"org.springframework/spring-beans":   {"org.springframework.beans"},
	"org.springframework/spring-web":     {"org.springframework.web"},
	"org.springframework/spring-webmvc":  {"org.springframework.web.servlet"},
	"org.springframework/spring-tx":      {"org.springframework.transaction"},
	"org.springframework/spring-orm":     {"org.springframework.orm"},
	"org.springframework/spring-aop":     {"org.springframework.aop"},
	"org.springframework/spring-jdbc":    {"org.springframework.jdbc"},
	"org.springframework/spring-test":    {"org.springframework.test"},
}

// mavenRuntimeDeps is a set of Maven "groupId/artifactId" coordinates for
// dependencies that are loaded via runtime mechanisms (reflection, ServiceLoader,
// classpath scanning) rather than static imports. Static source analysis cannot
// detect their usage, so they are recognized as runtime-scoped to prevent
// false-positive unused-dependency reports.
var mavenRuntimeDeps = map[string]struct{}{
	// JDBC drivers — loaded via java.sql.DriverManager / ServiceLoader.
	"mysql/mysql-connector-java":           {},
	"mysql/mysql-connector-j":              {},
	"org.postgresql/postgresql":            {},
	"com.h2database/h2":                    {},
	"org.mariadb.jdbc/mariadb-java-client": {},
	"com.oracle.database.jdbc/ojdbc11":     {},
	"com.microsoft.sqlserver/mssql-jdbc":   {},
	"org.xerial/sqlite-jdbc":               {},
	"com.amazon.redshift/redshift-jdbc42":  {},
	"org.hsqldb/hsqldb":                    {},
	"org.apache.derby/derby":               {},
	"com.ibm.db2/jcc":                      {},
	"org.firebirdsql.jdbc/jaybird":         {},
	"net.snowflake/snowflake-jdbc":         {},
	"com.clickhouse/clickhouse-jdbc":       {},
	"org.duckdb/duckdb_jdbc":               {},

	// Logging backends — loaded via SLF4J ServiceLoader / classpath binding.
	"ch.qos.logback/logback-classic":             {},
	"org.apache.logging.log4j/log4j-core":        {},
	"org.slf4j/slf4j-simple":                     {},
	"org.apache.logging.log4j/log4j-slf4j-impl":  {},
	"org.apache.logging.log4j/log4j-slf4j2-impl": {},

	// WebJars — served as classpath resources, never imported in Java source.
	"org.webjars/bootstrap":            {},
	"org.webjars/font-awesome":         {},
	"org.webjars/webjars-locator-lite": {},
	"org.webjars/webjars-locator-core": {},
	"org.webjars/jquery":               {},
	"org.webjars.npm/htmx.org":         {},
}

// buildMavenImportPaths generates candidate import path prefixes for a Maven PURL.
// It combines well-known overrides with heuristic candidates (groupId, groupId.artifactId).
func buildMavenImportPaths(parsed packageurl.PackageURL) []string {
	key := strings.ToLower(parsed.Namespace + "/" + parsed.Name)
	seen := make(map[string]struct{})
	var paths []string

	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}

	// 1. Well-known overrides take priority.
	for _, p := range mavenPackageOverrides[key] {
		add(p)
	}

	// 1b. Heuristic for unlisted Spring Boot starters: derive a prefix from
	// the starter suffix (e.g., "spring-boot-starter-data-jpa" →
	// "org.springframework.data.jpa"). This prevents all starters from
	// collapsing to the bare "org.springframework.boot" groupId (#295).
	if len(paths) == 0 &&
		strings.EqualFold(parsed.Namespace, "org.springframework.boot") &&
		strings.HasPrefix(strings.ToLower(parsed.Name), "spring-boot-starter-") {
		suffix := strings.TrimPrefix(strings.ToLower(parsed.Name), "spring-boot-starter-")
		if suffix != "" {
			candidate := "org.springframework." + strings.ReplaceAll(suffix, "-", ".")
			if isJavaDottedPackageSafe(candidate) {
				add(candidate)
			}
		}
	}

	// 2. groupId (namespace) — the most common convention.
	// Skip when the namespace contains characters invalid in Java package names
	// (e.g. "commons-io"), since such candidates can never match real imports.
	if isJavaDottedPackageSafe(parsed.Namespace) {
		add(parsed.Namespace)
	}

	// 3. groupId.artifactId — covers cases where the package mirrors the full coordinate.
	// Skip when namespace == name ignoring case (e.g. cglib/cglib or Cglib/cglib →
	// "cglib.cglib" is not a real package),
	// and skip when namespace or artifactId contains characters invalid in Java package names
	// (e.g. hyphens).
	if parsed.Namespace != "" && parsed.Name != "" &&
		!strings.EqualFold(parsed.Namespace, parsed.Name) &&
		isJavaDottedPackageSafe(parsed.Namespace) &&
		isJavaPackageSafe(parsed.Name) {
		add(parsed.Namespace + "." + parsed.Name)
	}

	if len(paths) == 0 {
		// Fallback to artifactId only when nothing else is available,
		// but only if it is a valid Java package segment.
		if isJavaPackageSafe(parsed.Name) {
			add(parsed.Name)
		}
	}

	return paths
}

// isJavaPackageSafe reports whether s is a valid Java package name segment.
// The first character must be a letter, underscore, or dollar sign; subsequent
// characters may also include digits.  Maven artifactIds often contain hyphens
// (e.g. "commons-lang3") or start with digits (e.g. "3scale") which are not
// valid in Java identifiers and would never match a real import statement.
func isJavaPackageSafe(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// isJavaDottedPackageSafe reports whether s is a valid dot-separated Java
// package prefix (e.g. "org.apache.commons").  Each segment between dots must
// satisfy isJavaPackageSafe.
func isJavaDottedPackageSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, seg := range strings.Split(s, ".") {
		if !isJavaPackageSafe(seg) {
			return false
		}
	}
	return true
}
