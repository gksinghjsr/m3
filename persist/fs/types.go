	"github.com/m3db/m3db/storage/namespace"
	xtime "github.com/m3db/m3x/time"
	Open(namespace ts.ID, blockSize time.Duration, shard uint32, start time.Time) error
	Open(md namespace.Metadata) error
	Open(md namespace.Metadata) error