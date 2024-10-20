package fugaci

import (
	regv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// MockImage is a mock implementation of the v1.Image interface.
type MockImage struct {
	mockDigest regv1.Hash
}

// NewMockImage creates a new MockImage with the given digest.
func NewMockImage(digestStr string) *MockImage {
	d, err := regv1.NewHash(digestStr)
	if err != nil {
		panic(err)
	}
	return &MockImage{
		mockDigest: d,
	}
}

// Digest returns the mock digest that was set when the MockImage was created.
func (m *MockImage) Digest() (regv1.Hash, error) {
	return m.mockDigest, nil
}

// Below are other unimplemented methods of v1.Image.
// You can add more if needed for your test cases.

func (m *MockImage) MediaType() (types.MediaType, error) {
	return "", nil
}

func (m *MockImage) ConfigName() (regv1.Hash, error) {
	return regv1.Hash{}, ErrNotImplemented
}

func (m *MockImage) RawConfigFile() ([]byte, error) {
	return nil, ErrNotImplemented
}

func (m *MockImage) ConfigFile() (*regv1.ConfigFile, error) {
	return nil, ErrNotImplemented
}

func (m *MockImage) LayerByDigest(d regv1.Hash) (regv1.Layer, error) {
	return nil, ErrNotImplemented
}

func (m *MockImage) Layers() ([]regv1.Layer, error) {
	return nil, ErrNotImplemented
}

func (m *MockImage) LayerByDiffID(h regv1.Hash) (regv1.Layer, error) {
	return nil, ErrNotImplemented
}

func (m *MockImage) RawManifest() ([]byte, error) {
	return nil, ErrNotImplemented
}

func (m *MockImage) Manifest() (*regv1.Manifest, error) {
	return nil, ErrNotImplemented
}

func (m *MockImage) Size() (int64, error) {
	return 0, ErrNotImplemented
}
