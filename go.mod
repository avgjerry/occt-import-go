module github.com/jerrylee1697/occt-import-go

go 1.25.0

require (
	github.com/klauspost/compress v1.19.2
	github.com/qmuntal/gltf v0.28.0
	// Pseudo-version of wazero main: v1.12.0's compiler engine has an
	// exception-handling bug ("invalid table access" on throw) fixed after
	// the release. Move to v1.12.1+ once tagged.
	github.com/tetratelabs/wazero v1.12.1-0.20260804131901-3ab421731a94
)

require golang.org/x/sys v0.44.0 // indirect
