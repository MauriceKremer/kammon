package web

// localPlaces supplements Natural Earth's 10m populated places around the demo base. Natural Earth
// stops above this tier: Emmen and Coevorden are not in the dataset at all, so the local cluster is
// vendored here at the same NAME|lon|lat|rank format as cityPlaces. Ranks 7 and 8 keep them in the
// map's existing priority reveal; names are static vendored data, never radio text (D54).
const localPlaces = `Emmen|6.897|52.785|7
Coevorden|6.74|52.661|7
Hoogeveen|6.476|52.722|7
Meppel|6.194|52.696|7
Hardenberg|6.619|52.575|7
Klazienaveen|6.99|52.724|8
Borger|6.793|52.923|8
Ter Apel|7.059|52.876|8`
