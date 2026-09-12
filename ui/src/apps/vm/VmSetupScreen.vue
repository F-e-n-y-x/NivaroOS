<template>
	<div class="vm-setup">
		<div v-if="checking" class="has-text-centered py-6">
			<b-icon icon="loading" custom-class="mdi-spin" size="is-large"></b-icon>
		</div>
		<div v-else class="vm-setup-card">
			<h2 class="section-title">{{ $t('Set up virtual machines') }}</h2>
			<p class="mb-4">{{ $t('A few one-time components need to be installed on this server before you can create VMs.') }}</p>

			<div v-if="unreachable" class="has-text-centered mb-4">
				<p class="has-text-danger mb-3">{{ $t("VM support isn't installed on this server yet.") }}</p>
				<b-button type="is-primary" outlined class="install-btn" @click="openInstallTerminal">
					{{ $t('Open terminal to install') }}
				</b-button>
				<p class="vm-setup-hint mt-3">{{ $t('This opens a terminal with the install command already typed in - just press Enter.') }}</p>
			</div>
			<template v-else>
				<div v-if="status.missing_packages && status.missing_packages.length" class="mb-4">
					<p class="has-text-weight-semibold">{{ $t('Still need to install:') }}</p>
					<ul class="vm-setup-missing">
						<li v-for="pkg in status.missing_packages" :key="pkg">{{ pkg }}</li>
					</ul>
				</div>
				<p v-if="!status.libvirt_reachable" class="has-text-danger mb-4">
					{{ $t("The virtualization service isn't responding yet - try installing again, or restart the server if this persists.") }}
				</p>
			</template>

			<b-message v-if="installResult && !installResult.success" type="is-danger" :closable="false">
				<strong>{{ installResult.step }}</strong>
				<pre class="vm-setup-output">{{ installResult.output }}</pre>
			</b-message>

			<b-button v-if="!unreachable" type="is-primary" class="install-btn" :loading="installing" @click="install">
				{{ $t('Install now') }}
			</b-button>
		</div>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'

export default {
	name: 'vm-setup-screen',
	data() {
		return {
			checking: true,
			installing: false,
			status: { missing_packages: [], libvirt_reachable: false, ready: false },
			unreachable: false,
			installResult: null
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		async refresh() {
			this.checking = true
			try {
				this.status = await vmSidecar.getSetupStatus()
				this.unreachable = false
				if (this.status.ready) this.$emit('ready')
			} catch (e) {
				this.unreachable = true
			} finally {
				this.checking = false
			}
		},
		async install() {
			this.installing = true
			this.installResult = null
			try {
				this.installResult = await vmSidecar.runSetupInstall()
			} catch (e) {
				this.installResult = { step: 'request failed', output: e.message, success: false }
			} finally {
				this.installing = false
				await this.refresh()
			}
		},
		openInstallTerminal() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-vm-enable-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480,
				props: { initCommand: 'nivaroos-cli vm enable' }
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.vm-setup {
	padding: var(--space-5);
}

// Matches the card treatment used everywhere else in this app
// (iso-row/network-row/vm-card) - this screen had none at all, so its
// content floated unstyled directly on the window background.
.vm-setup-card {
	max-width: 28rem;
	margin: var(--space-8) auto 0;
	padding: var(--space-6);
	border: 1px solid var(--color-border-strong);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #fff);
}
.section-title {
	font-size: var(--font-lg);
	font-weight: 700;
	color: var(--theme-text-primary, #1e293b);
	margin: 0 0 var(--space-3);
}

// Same flat, borderless button used everywhere else in this app -
// <b-button> on its own renders Bulma's stock bordered/white look.
::v-deep .install-btn {
	border: none;
	border-radius: var(--radius-sm);
	font-weight: 500;
	font-size: var(--font-sm);
	padding: 0 var(--space-3);
	height: 2rem;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	background: var(--color-primary);
	box-shadow: none;

	&:hover {
		background: var(--color-primary-hover);
	}
}

// The "open terminal" path is outlined, not filled - it's a detour to a
// different tool, not the primary action, so it shouldn't compete with it.
::v-deep .b-button.install-btn.is-outlined {
	background: transparent;
	color: var(--color-primary);
	border: 1px solid var(--color-primary);

	&:hover {
		background: var(--color-primary-soft);
	}
}

.vm-setup-hint {
	font-size: var(--font-xs);
	color: var(--color-text-muted);
}

.vm-setup-missing {
	list-style: disc;
	padding-left: var(--space-6);
	font-family: monospace;
	font-size: var(--font-base);
}

.vm-setup-output {
	white-space: pre-wrap;
	font-size: var(--font-sm);
	margin-top: var(--space-2);
}
</style>
