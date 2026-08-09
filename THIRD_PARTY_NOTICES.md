# Third-party notices

## Open CASCADE Technology (OCCT)

The embedded WebAssembly module (`internal/wasm/occt.wasm.zst`) contains
Open CASCADE Technology, compiled from the unmodified V7_9_1 sources.

OCCT is licensed under the **GNU Lesser General Public License version 2.1
with the Open CASCADE Exception 1.0**, which permits statically linking and
distributing OCCT as part of a larger work. See:

- https://github.com/Open-Cascade-SAS/OCCT/blob/master/LICENSE_LGPL_21.txt
- https://github.com/Open-Cascade-SAS/OCCT/blob/master/OCCT_LGPL_EXCEPTION.txt

The complete corresponding source and the exact build recipe for the wasm
module are in this repository under `wasm/` (see `wasm/build_occt.sh`, which
fetches the pinned, checksummed OCCT release).

## RapidJSON

The wasm module includes RapidJSON (MIT license), used by OCCT's glTF writer.
https://github.com/Tencent/rapidjson

## Test fixtures

See `testdata/README.md` for the provenance and licenses of the STEP sample
files used by the test suite.
