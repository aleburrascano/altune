package memory

import (
	"errors"
	"fmt"
	"net/url"
)

var UpstreamDown = &url.Error{Op: "Get", URL: "https://provider.test", Err: errors.New("connection refused")}

type StatusErr int

func (e StatusErr) Error() string { return fmt.Sprintf("http status %d", int(e)) }

func (e StatusErr) UpstreamStatus() int { return int(e) }
