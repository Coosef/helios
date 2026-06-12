package mocksaas

import (
	"net/http"
	"net/http/httptest"

	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// Server serves a fixture's signed manifest at ManifestPath and its artifact bytes
// under /artifacts/<name>.bin, over plain HTTP. The artifact channel is UNPINNED —
// integrity comes from the signed hashes (T25), not the transport.
//
// The signed manifest carries a canonical https placeholder artifact URL (so it
// passes manifest.Validate); the real, resolvable download URL on this server is
// Server.ArtifactURL(). The signed manifest bytes are served byte-for-byte (never
// rewritten — that would break the signature).
type Server struct {
	ts   *httptest.Server
	fx   Fixture
	path string // /artifacts/<name>.bin
}

// NewServer starts a server for the named fixture. Call Close when done.
func NewServer(fixtureName string) *Server { return NewServerFor(ByName(fixtureName)) }

// NewServerFor starts a server for a specific fixture value.
func NewServerFor(f Fixture) *Server {
	s := &Server{fx: f, path: artifactPath + "/" + f.Name + ".bin"}
	mux := http.NewServeMux()
	mux.HandleFunc(ManifestPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.fx.Manifest)
	})
	mux.HandleFunc(s.path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(s.fx.Artifact)
	})
	s.ts = httptest.NewServer(mux)
	return s
}

// BaseURL is the server root (e.g. http://127.0.0.1:PORT).
func (s *Server) BaseURL() string { return s.ts.URL }

// ManifestURL is the absolute URL the updater fetches the manifest from.
func (s *Server) ManifestURL() string { return s.ts.URL + ManifestPath }

// ArtifactURL is the absolute, resolvable URL the served artifact lives at.
func (s *Server) ArtifactURL() string { return s.ts.URL + s.path }

// Artifact parses the served manifest and returns the artifact for platform/arch
// with its URL RESOLVED to this server (so callers never hit the https placeholder).
// This removes the footgun of hand-rewriting art.URL before swap.Stage.
func (s *Server) Artifact(platform, arch string) (manifest.Artifact, bool) {
	m, err := manifest.Parse(s.fx.Manifest)
	if err != nil {
		return manifest.Artifact{}, false
	}
	a, ok := m.ArtifactFor(platform, arch)
	if !ok {
		return manifest.Artifact{}, false
	}
	a.URL = s.ArtifactURL()
	return a, true
}

// Client is an *http.Client that can reach this server.
func (s *Server) Client() *http.Client { return s.ts.Client() }

// Close shuts the server down.
func (s *Server) Close() { s.ts.Close() }
