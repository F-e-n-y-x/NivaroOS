<template>
	<div class="modal-card">
		<!-- Modal-Card Body Start -->
		<section class="modal-card-body">
			<VMdEditor v-model="tips" :mode="controlEditorState" :placeholder="$t('Something to remember eg. password')"
				left-toolbar right-toolbar>
			</VMdEditor>
			<div v-if="name" class="is-flex is-flex-direction-row-reverse mt-2">
				<button type="button" class="tips-toggle-btn"
					:class="{ 'is-editing': isEditing, 'is-changed': isDifferentiation }"
					:aria-label="isEditing ? $t('Save tips') : $t('Edit tips')"
					:title="isEditing ? $t('Save tips') : $t('Edit tips')"
					:aria-pressed="isEditing ? 'true' : 'false'"
					:disabled="isSaving"
					@click="toggle">
					<b-icon :icon="icon" pack="casa" aria-hidden="true"></b-icon>
				</button>
			</div>
		</section>
		<!-- Modal-Card Body End -->

		<!-- Modal-Card Footer Start-->
		<footer v-if="!name" class="modal-card-foot is-flex is-align-items-center">
			<div class="is-flex-grow-1"></div>
			<div class="is-flex tips-footer-actions">
				<b-button rounded size="is-small" @click="handleCancel">{{ $t('Cancel') }}</b-button>
				<b-button rounded size="is-small" type="is-primary" @click="handleSubmit">{{ $t('Next Steps') }}
				</b-button>
			</div>
		</footer>
		<!-- Modal-Card Footer End-->
	</div>
</template>

<script>
import YAML from "yaml";
import merge from "lodash/merge";
import cloneDeep from "lodash/cloneDeep";
import { apiErrorHtml } from "@/mixins/app/apiError";
import { escapeHtml } from "@/utils/escapeHtml";
import VMdEditor from '@kangc/v-md-editor';
import '@kangc/v-md-editor/lib/style/base-editor.css';
import githubTheme from '@kangc/v-md-editor/lib/theme/github.js';
import '@kangc/v-md-editor/lib/theme/style/github.css';
import hljs from 'highlight.js';
import { ice_i18n } from "@/mixins/base/common-i18n";

VMdEditor.use(githubTheme, {
	Hljs: hljs,
	// extend(md) {},
});

export default {
	name: "TipEditorModal",
	components: {
		VMdEditor
	},
	data() {
		return {
			isEditing: false,
			tips: '',
			tempTips: '',
			controlEditorState: 'preview',
			icon: 'edit-outline',
			isSaving: false,
			// Set once the user picked Next Steps or Cancel, so closing the
			// window afterwards doesn't fire onCancel a second time.
			responded: false
		}
	},
	props: {
		composeData: {
			type: Object,
			required: true
		},
		name: {
			type: String,
			// required: true
		},
		// Called directly when opened as a standalone desktop window - the
		// shared window chrome only forwards close/minimize, not custom
		// business events like 'submit'.
		onSubmit: {
			type: Function,
			default: null
		},
		// Called on Cancel and when the window is closed without choosing
		// (title-bar X) - lets the opener reset its "Installing" state.
		onCancel: {
			type: Function,
			default: null
		},
		// The opener passes a per-app window id (e.g. `tip-editor-<appId>`)
		// so two pending installs don't share one window; kept here so the
		// component can close exactly its own window.
		windowId: {
			type: String,
			default: ''
		}
	},
	computed: {
		isDifferentiation() {
			return this.tempTips !== this.tips
		},
	},
	watch: {
		isEditing(val) {
			if (val) {
				// editor is editable
				this.controlEditorState = 'edit'
				this.icon = 'check-outline'
			} else {
				// editor is not editable
				this.controlEditorState = 'preview'
				this.icon = 'edit-outline'
			}
			return this.isEditing
		},
		composeData: {
			handler() {
				//Get tips in compose.
				let getValueByPath = this.composeData['x-casaos']
				if (getValueByPath?.['tips']?.['custom'] || getValueByPath?.['tips']?.['before_install']) {
					this.tips = getValueByPath['tips']['custom'] || ice_i18n(getValueByPath['tips']['before_install'])
				} else {
					this.tips = '';
				}
				// init tempTips
				this.tempTips = this.tips;
			},
			immediate: true
		}
	},
	beforeDestroy() {
		// Closed via the window chrome without Next Steps / Cancel.
		if (!this.name && !this.responded) {
			this.responded = true
			this.runCallback(this.onCancel)
		}
	},
	methods: {
		runCallback(fn) {
			if (typeof fn !== 'function') return
			try {
				const res = fn()
				if (res && typeof res.catch === 'function') {
					res.catch(err => console.error('TipEditorModal callback', err))
				}
			} catch (err) {
				console.error('TipEditorModal callback', err)
			}
		},
		handleSubmit() {
			if (this.responded) return
			this.responded = true
			this.$emit('submit')
			this.runCallback(this.onSubmit)
			this.$emit('close')
		},
		handleCancel() {
			if (this.responded) return
			this.responded = true
			this.$emit('cancel')
			this.runCallback(this.onCancel)
			this.$emit('close')
		},
		/*
		* 1、进入编辑状态
		* 2、保存
		* */
		toggle() {
			this.isEditing = !this.isEditing
			if (!this.isEditing && this.isDifferentiation) {
				this.save();
			}
		},

		save() {
			const previous = this.tempTips
			this.tempTips = this.tips
			this.isSaving = true
			let realComposeData = this.getCompleteComposeData()
			this.$openAPI.appManagement.compose.applyComposeAppSettings(this.name, YAML.stringify(realComposeData)).then(res => {
				if (res.status === 200) {
					this.$buefy.toast.open({
						message: escapeHtml((res.data && res.data.message) || this.$t('Tips saved')),
						type: 'is-success',
						position: 'is-top',
						duration: 5000
					})
				}
			}).catch(e => {
				console.error('Error in saving tips:', e)
				// Not saved: keep the edit marked as unsaved.
				this.tempTips = previous
				this.$buefy.toast.open({
					message: apiErrorHtml(e, this.$t('Unable to save the tips')),
					type: 'is-danger',
					position: 'is-top',
					duration: 5000
				})
			}).finally(() => {
				this.isSaving = false
			})
		},
		getCompleteComposeData() {
			/*let lines = this.tips.split('\n');
			let body = [];

			lines.forEach(line => {
				let splitArray = line.split(':');
				let value = splitArray.length > 1 ? splitArray[0] : 'user input';
				let content = splitArray.length > 1 ? splitArray[1] : splitArray[0];
				body.push({value, content: {default: content}});
			});*/

			// Clone first: never mutate the composeData prop.
			let result = merge(cloneDeep(this.composeData), {
				'x-casaos': {
					tips: {
						custom: this.tips
					}
				}
			})
			return result
		}
	},
}
</script>

<style lang="scss" scoped>
.modal-card {
	/* v0.4.3 */
	width: 26.5rem;
	max-width: 100%;

	.modal-card-head {
		padding-top: var(--space-5);
		border-bottom: 1px solid var(--theme-card-border) !important;

		.close {
			height: 2rem;
			width: 2rem;
			border-radius: var(--radius-sm);
		}
	}

	.tips-footer-actions {
		gap: var(--space-2);
	}

	.tips-toggle-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		padding: 0;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--theme-text-secondary);
		cursor: pointer;

		&:hover:not(:disabled) {
			background: var(--theme-card-hover);
		}

		&:focus-visible {
			outline: 2px solid var(--color-primary-fg);
			outline-offset: 2px;
		}

		&.is-editing {
			color: var(--theme-text-muted);
		}

		&.is-editing.is-changed {
			color: var(--color-success-fg);
		}

		&:disabled {
			opacity: 0.5;
			cursor: default;
		}
	}

	.modal-card-body {
		padding: var(--space-6);

		::v-deep .v-md-editor {
			box-shadow: none;
			border: 1px solid var(--theme-card-border);
			border-radius: var(--radius-sm);

			overflow: hidden;
			resize: vertical;
			max-height: 20.25rem;
			min-height: 5.25rem;

			&.v-md-editor--edit {
				/* 覆盖上层 */
				border: 0;

				.scrollbar__wrap {
					border: 1px solid var(--theme-input-focus);
					border-radius: var(--radius-control);
				}
			}

			.v-md-editor__right-area {
				.v-md-editor__toolbar {
					display: none;
					padding: 0;
					border: 0;
				}

				.v-md-editor__main {
					.v-md-textarea-editor textarea {
						padding: var(--space-3) var(--space-4);
						/* Text 400Regular/Text03 */

						font-family: $family-sans-serif;
						font-style: normal;
						font-weight: 400;
						font-size: var(--font-base);
						line-height: 20px;
						/* identical to box height, or 143% */

						font-feature-settings: 'pnum' on, 'lnum' on;

					}
				}
			}
		}

		/*textarea {
				resize: none;
				height: 5.25rem;
		}*/
	}
}
</style>