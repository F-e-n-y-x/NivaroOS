<template>
	<div class="vm-setup">
		<div v-if="checking" class="has-text-centered py-6">
			<b-icon icon="loading" custom-class="mdi-spin" size="is-large"></b-icon>
		</div>
		<div v-else class="vm-setup-card">
			<h2 class="section-title">{{ $t('Set up virtualization') }}</h2>
			<p class="mb-4">{{ $t('QEMU/KVM and libvirt are required to create and run VMs.') }}</p>

			<div v-if="status.missing_packages && status.missing_packages.length" class="mb-4">
				<p class="has-text-weight-semibold">{{ $t('Missing packages') }}:</p>
				<ul class="vm-setup-missing">
					<li v-for="pkg in status.missing_packages" :key="pkg">{{ pkg }}</li>
				</ul>
			</div>
			<p v-if="!status.libvirt_reachable" class="has-text-danger mb-4">
				{{ $t('libvirtd is not reachable.') }}
			</p>

			<b-message v-if="installResult && !installResult.success" type="is-danger" :closable="false">
				<strong>{{ installResult.step }}</strong>
				<pre class="vm-setup-output">{{ installResult.output }}</pre>
			</b-message>

			<b-button type="is-primary" class="install-btn" :loading="installing" @click="install">
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
				if (this.status.ready) this.$emit('ready')
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
		}
	}
}
</script>

<style lang="scss" scoped>
.vm-setup {
	padding: 1.25rem;
}

// Matches the card treatment used everywhere else in this app
// (iso-row/network-row/vm-card) - this screen had none at all, so its
// content floated unstyled directly on the window background.
.vm-setup-card {
	max-width: 28rem;
	margin: 2rem auto 0;
	padding: 1.5rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 12px;
	background: #fff;
}
.section-title {
	font-size: 1.1rem;
	font-weight: 700;
	color: #2c3e50;
	margin: 0 0 0.75rem;
}

// Same flat, borderless button used everywhere else in this app -
// <b-button> on its own renders Bulma's stock bordered/white look.
::v-deep .install-btn {
	border: none;
	border-radius: 8px;
	font-weight: 600;
	font-size: 0.85rem;
	padding: 0.55rem 1rem;
	height: auto;
	background: #3273dc;
	box-shadow: none;

	&:hover {
		background: #2366d1;
	}
}

.vm-setup-missing {
	list-style: disc;
	padding-left: 1.5rem;
	font-family: monospace;
	font-size: 0.85rem;
}

.vm-setup-output {
	white-space: pre-wrap;
	font-size: 0.8rem;
	margin-top: 0.5rem;
}
</style>
