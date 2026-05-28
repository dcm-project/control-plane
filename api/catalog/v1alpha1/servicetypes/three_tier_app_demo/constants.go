package three_tier_app_demo

// Default database engine and version for DatabaseTier.
// Used by seed and catalog field configs for three_tier_app_demo.
const (
	DefaultDatabaseEngine  = "postgres"
	DefaultDatabaseVersion = "18"

	// constants for the three_tier_app_demo Pet Clinic CatalogItem
	WebImage = "registry.access.redhat.com/ubi9/nginx-126:9.7"
	AppImage = "docker.io/springcommunity/spring-framework-petclinic:6.1.2"
)
