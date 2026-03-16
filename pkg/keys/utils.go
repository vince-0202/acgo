package keys

// IdKind specifies the type of ID to generate (used with utils.IdGenerator).
type IdKind string

const (
	IdKindUUID      IdKind = "uuid"
	IdKindSnowflake IdKind = "snowflake"
)
