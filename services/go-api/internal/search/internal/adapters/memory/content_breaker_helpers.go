package memory

import (
	"errors"
	"fmt"
	"net/url"
)

var ErrUpstreamDown = &url.Error{Op: "Get", URL: "https://provider.test", Err: errors.New("connection refused")}

type UpstreamStatusError int

func (e UpstreamStatusError) Error() string { return fmt.Sprintf("http status %d", int(e)) }

func (e UpstreamStatusError) UpstreamStatus() int { return int(e) }
