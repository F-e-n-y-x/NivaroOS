// Icon packs for <b-icon> (ui/Icon.vue): the class prefix and the size
// class each `size` maps to. The casa
// internalIcons are for the Buefy components still in use (their `both` icons).
export const iconPacks = {
	'casa': {
		sizes: {
			'default': 'is-size-4',
			'is-20': 'is-size-5',
			'is-small': '',
			'is-medium': 'is-size-3',
			'is-large': 'is-size-1'
		},
		iconPrefix: 'casa-',
		internalIcons: {
			'check': 'checkmark',
			'information': 'information',
			'check-circle': 'checkmark-circle-outline',
			'alert': 'alert',
			'alert-circle': 'alert',
			'arrow-up': 'arrow-up',
			'chevron-right': 'arrow-right',
			'chevron-left': 'arrow-back',
			'chevron-down': 'arrow-down',
			'eye': 'eye',
			'eye-off': 'eye-off',
			'menu-down': 'arrow-dropdown',
			'menu-up': 'arrow-dropup',
			'close-circle': 'close-circle-outline'
		}
	},
	// Without this, size="is-XX" on a pack="mdi" b-icon resolves to
	// nothing (only 'casa' had a sizes map), so the <i> glyph falls back
	// to the MDI font's unconstrained default (~24px) regardless of the
	// size requested - next to a properly-sized casa icon in the same
	// row (e.g. the Settings sidebar nav) it visibly renders larger and
	// can overflow/clip its container. Mirrors casa's own size keys,
	// mapped to this project's .mdi-XXpx utility classes (_size.scss).
	'mdi': {
		iconPrefix: 'mdi-',
		sizes: {
			'default': 'mdi-24px',
			'is-20': 'mdi-20px',
			'is-16': 'mdi-16px',
			'is-14': 'mdi-14px',
			'is-small': 'mdi-16px',
			'is-medium': 'mdi-36px',
			'is-large': 'mdi-48px'
		}
	}
}
