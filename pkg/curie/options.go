package curie

type Options struct {
	Headless bool
}

type Option func(options *Options)

func WithHeadless(headless bool) Option {
	return func(options *Options) {
		options.Headless = headless
	}
}

func NewOptions(opts ...Option) *Options {
	options := &Options{}
	for _, o := range opts {
		o(options)
	}
	return options
}
