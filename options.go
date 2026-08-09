package occt

// options are the tessellation parameters for a conversion.
type options struct {
	linearDeflection   float64
	angularDeflection  float64
	relativeDeflection bool
}

// defaultOptions matches occt-import-js's defaults: linear deflection as a
// bounding-box ratio, so mesh quality is independent of model scale.
func defaultOptions() options {
	return options{
		linearDeflection:   0.001,
		angularDeflection:  0.5,
		relativeDeflection: true,
	}
}

// Option configures a single conversion.
type Option func(*options)

// WithLinearDeflection sets the chordal deflection for meshing. With
// WithRelativeDeflection(true) (the default) it is a fraction of the shape's
// bounding box; otherwise it is an absolute distance in model units
// (millimeters for most STEP files). Smaller values produce finer meshes.
func WithLinearDeflection(d float64) Option {
	return func(o *options) { o.linearDeflection = d }
}

// WithAngularDeflection sets the angular deflection for meshing, in radians.
// Smaller values produce finer meshes on curved surfaces.
func WithAngularDeflection(d float64) Option {
	return func(o *options) { o.angularDeflection = d }
}

// WithRelativeDeflection chooses whether linear deflection is relative to
// each shape's bounding box (true, default) or absolute in model units.
func WithRelativeDeflection(relative bool) Option {
	return func(o *options) { o.relativeDeflection = relative }
}
