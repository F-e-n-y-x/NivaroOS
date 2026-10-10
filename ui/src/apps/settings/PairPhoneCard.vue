<template>
	<div class="setting-card pair-phone-card">
		<div class="setting-row">
			<b-icon class="row-icon" icon="qrcode" pack="mdi" size="is-20"></b-icon>
			<div class="row-label">
				<div class="setting-title">{{ $t('Pair a phone') }}</div>
				<div class="setting-desc">
					{{ $t('Sign the NivaroOS app in to your account without typing the password: scan the QR code, or enter the code, in the app under Add server.') }}
				</div>
			</div>
			<div class="row-control">
				<b-button rounded size="is-small" type="is-primary" :loading="loading" @click="newCode">
					{{ code ? $t('New code') : $t('Show code') }}
				</b-button>
			</div>
		</div>
		<div v-if="error" class="setting-row pair-error" role="alert">{{ error }}</div>
		<div v-if="code" class="setting-row pair-body">
			<div class="pair-qr" :class="{ 'is-expired': !left }" role="img" :aria-label="$t('QR code for pairing a phone')" v-html="qrSvg"></div>
			<div class="pair-info">
				<div class="pair-code" :class="{ 'is-expired': !left }" aria-live="polite">{{ shownCode }}</div>
				<div class="setting-desc">
					<template v-if="left">{{ $t('Valid for {time}, once.', { time: countdown }) }}</template>
					<template v-else>{{ $t('This code expired. Show a new one.') }}</template>
				</div>
				<div v-if="addresses.length" class="setting-desc mt-2">
					{{ $t('The QR code also tells the app where to find this server:') }}
					<ul class="pair-addresses">
						<li v-for="a in addresses" :key="a">{{ a }}</li>
					</ul>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import { renderSVG } from 'uqr'
import { formatCountdown, formatPairCode, pairAddresses, pairPayload, secondsLeft } from '@/utils/pairPhone'

export default {
	name: 'pair-phone-card',
	data() {
		return { code: '', expiresAt: 0, addresses: [], now: Date.now(), loading: false, error: '', timer: null }
	},
	computed: {
		left() {
			return secondsLeft(this.expiresAt, this.now)
		},
		countdown() {
			return formatCountdown(this.left)
		},
		shownCode() {
			return formatPairCode(this.code)
		},
		// Our own data only (code + addresses), drawn by uqr as SVG markup.
		qrSvg() {
			return this.code ? renderSVG(pairPayload(this.code, this.addresses), { border: 2 }) : ''
		}
	},
	beforeUnmount() {
		clearInterval(this.timer)
	},
	methods: {
		async findAddresses() {
			const data = (p) => p.then(r => r?.data?.data).catch(() => null)
			const [port, ifaces, ts] = await Promise.all([
				data(this.$api.sys.getServerPort()),
				data(this.$api.sys.getNetworkInterfaces()),
				data(this.$api.tailscale.getStatus())
			])
			return pairAddresses({
				origin: window.location.origin,
				port: port || window.location.port,
				lanIps: (ifaces || []).map(i => i.ip),
				tailscaleIps: ts?.TailscaleIPs || [],
				tailscaleName: ts?.Self?.DNSName || ''
			})
		},
		async newCode() {
			this.loading = true
			this.error = ''
			try {
				const [res, addresses] = await Promise.all([this.$api.users.createPairCode(), this.findAddresses()])
				this.code = res.data.data.code
				this.expiresAt = res.data.data.expires_at
				this.addresses = addresses
				this.now = Date.now()
				clearInterval(this.timer)
				this.timer = setInterval(() => {
					this.now = Date.now()
					if (!this.left) clearInterval(this.timer)
				}, 1000)
			} catch (e) {
				this.error = e?.response?.data?.message || this.$t('Could not create a pairing code.')
			} finally {
				this.loading = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.pair-body {
	align-items: flex-start;
	gap: 1.25rem;
	flex-wrap: wrap;
}
.pair-qr {
	width: 168px;
	height: 168px;
	flex: none;
	background: #fff; // scanners want dark on light, also in dark mode
	border-radius: 8px;
	padding: 4px;
	:deep(svg) {
		width: 100%;
		height: 100%;
		display: block;
	}
	&.is-expired {
		opacity: 0.2;
	}
}
.pair-info {
	flex: 1;
	min-width: 12rem;
}
.pair-code {
	font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
	font-size: 1.75rem;
	font-weight: 600;
	letter-spacing: 0.12em;
	color: var(--theme-text-primary);
	&.is-expired {
		text-decoration: line-through;
		color: var(--theme-text-muted);
	}
}
.pair-addresses {
	margin: 0.25rem 0 0 1rem;
	list-style: disc;
	word-break: break-all;
}
.pair-error {
	color: var(--theme-danger, #dc2626);
	font-size: 0.85rem;
}
</style>
