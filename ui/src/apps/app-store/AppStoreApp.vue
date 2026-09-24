<template>
	<div class="appstore-app" :class="{ 'is-compact': isCompact, 'is-narrow': isNarrow }">
		<!-- Left Navigation Sidebar -->
		<aside class="appstore-sidebar" :aria-label="$t('App Store sections')">
			<div class="sidebar-brand">
				<img :src="appStoreIcon" class="brand-icon" alt="App Store" />
				<div class="brand-info">
					<h2 class="brand-title">{{ $t('App Store') }}</h2>
					<span class="brand-subtitle">{{ totalAppCount }} {{ $t('Applications') }}</span>
				</div>
			</div>

			<div class="sidebar-nav">
				<button
					type="button"
					class="nav-item"
					:class="{ 'is-active': viewMode === 'store' && activeTab === 'discover' }"
					:aria-current="viewMode === 'store' && activeTab === 'discover' ? 'page' : null"
					:aria-label="isCompact ? $t('Discover') : null"
					:title="isCompact ? $t('Discover') : null"
					@click="switchToStoreTab('discover')"
				>
					<span class="nav-icon"><i class="mdi mdi-compass-outline"></i></span>
					<span class="nav-label">{{ $t('Discover') }}</span>
				</button>

				<button
					type="button"
					class="nav-item"
					:class="{ 'is-active': viewMode === 'store' && activeTab === 'all' }"
					:aria-current="viewMode === 'store' && activeTab === 'all' ? 'page' : null"
					:aria-label="isCompact ? $t('All Apps') : null"
					:title="isCompact ? $t('All Apps') : null"
					@click="switchToStoreTab('all')"
				>
					<span class="nav-icon"><i class="mdi mdi-view-grid-outline"></i></span>
					<span class="nav-label">{{ $t('All Apps') }}</span>
					<span class="nav-count">{{ allAppsList.length }}</span>
				</button>

				<div class="nav-section-header">
					<span>{{ $t('CATEGORIES') }}</span>
				</div>

				<div class="category-list">
					<button
						v-for="cat in sidebarCategories"
						:key="cat.id"
						type="button"
						class="nav-item category-item"
						:class="{ 'is-active': viewMode === 'store' && activeTab === 'category' && currentCate.name === cat.name }"
						:aria-current="viewMode === 'store' && activeTab === 'category' && currentCate.name === cat.name ? 'page' : null"
						:aria-label="isCompact ? cat.name : null"
						:title="isCompact ? cat.name : null"
						@click="selectCategory(cat)"
					>
						<span class="nav-icon"><i :class="'mdi mdi-' + getCateIcon(cat.name)"></i></span>
						<span class="nav-label">{{ cat.name }}</span>
						<span class="nav-count">{{ cat.count }}</span>
					</button>
				</div>

				<div class="nav-section-header">
					<span>{{ $t('LIBRARY') }}</span>
				</div>

				<button
					type="button"
					class="nav-item"
					:class="{ 'is-active': viewMode === 'store' && activeTab === 'installed' }"
					:aria-current="viewMode === 'store' && activeTab === 'installed' ? 'page' : null"
					:aria-label="isCompact ? $t('Installed') : null"
					:title="isCompact ? $t('Installed') : null"
					@click="switchToStoreTab('installed')"
				>
					<span class="nav-icon"><i class="mdi mdi-check-circle-outline"></i></span>
					<span class="nav-label">{{ $t('Installed') }}</span>
					<span class="nav-count">{{ installedList.length }}</span>
				</button>
			</div>

			<div class="sidebar-footer">
				<button type="button" class="footer-btn custom-install-btn" :class="{ 'is-active': viewMode === 'installer' }" :aria-label="isCompact ? $t('Custom Install') : null" :title="isCompact ? $t('Custom Install') : null" @click="openCustomInstall">
					<i class="mdi mdi-plus footer-icon"></i>
					<span>{{ $t('Custom Install') }}</span>
				</button>

				<button type="button" class="footer-btn sources-btn" :aria-label="isCompact ? $t('App Sources') : null" :title="isCompact ? $t('App Sources') : null" @click="showSourcesModal = true">
					<i class="mdi mdi-source-branch footer-icon"></i>
					<span>{{ $t('App Sources') }}</span>
				</button>
			</div>
		</aside>

		<!-- Mode 1: Main App Store Browser -->
		<main v-if="viewMode === 'store'" class="appstore-main">
			<!-- Top Toolbar -->
			<header class="main-header">
				<div class="search-wrapper">
					<i class="mdi mdi-magnify search-icon"></i>
					<input
						v-model="searchQuery"
						type="search"
						class="search-input"
						:aria-label="$t('Search apps')"
						:placeholder="$t('Search {n} apps...', { n: totalAppCount || '' })"
						@input="onSearchInput"
						@keydown.esc="clearSearch"
					/>
					<button v-if="searchQuery" type="button" class="clear-search-btn" :aria-label="$t('Clear search')" :title="$t('Clear search')" @click="clearSearch">
						<i class="mdi mdi-close-circle"></i>
					</button>
				</div>

				<div class="header-actions">
					<!-- Store Sources Dropdown -->
					<b-dropdown v-model="currentAuthor" aria-role="list" class="source-dropdown">
						<template #trigger="{ active }">
							<button type="button" class="filter-btn" :aria-label="$t('Publisher: {name}', { name: $t(currentAuthor.name) })">
								<i class="mdi mdi-account-check-outline" aria-hidden="true"></i>
								<span>{{ currentAuthor.name === 'All' ? $t('All publishers') : $t(currentAuthor.name) }}</span>
								<i :class="'mdi ' + (active ? 'mdi-chevron-up' : 'mdi-chevron-down')"></i>
							</button>
						</template>
						<b-dropdown-item
							v-for="item in authorMenu"
							:key="item.name"
							:value="item"
							:class="{ 'is-active': currentAuthor.name === item.name }"
						>
							{{ item.name === 'All' ? $t('All publishers') : $t(item.name) }}
						</b-dropdown-item>
					</b-dropdown>

					<!-- Sort Dropdown -->
					<b-dropdown v-model="currentSort" aria-role="list" class="sort-dropdown">
						<template #trigger="{ active }">
							<button type="button" class="filter-btn" :aria-label="$t('Sort: {name}', { name: $t(currentSort.name) })">
								<i class="mdi mdi-sort-variant" aria-hidden="true"></i>
								<span>{{ $t(currentSort.name) }}</span>
								<i :class="'mdi ' + (active ? 'mdi-chevron-up' : 'mdi-chevron-down')"></i>
							</button>
						</template>
						<b-dropdown-item
							v-for="item in sortMenu"
							:key="item.name"
							:value="item"
							:class="{ 'is-active': currentSort.name === item.name }"
						>
							{{ $t(item.name) }}
						</b-dropdown-item>
					</b-dropdown>

					<!-- Refresh Button -->
					<button type="button" class="icon-btn refresh-btn" :class="{ 'is-spinning': isLoading }" :title="$t('Refresh Store')" :aria-label="$t('Refresh Store')" :disabled="isLoading" @click="refreshStore">
						<i class="mdi mdi-refresh"></i>
					</button>
				</div>
			</header>

			<!-- Phone width: the sidebar is hidden, so its sections live here. -->
			<nav v-if="isNarrow" class="narrow-nav" :aria-label="$t('App Store sections')">
				<label class="sr-only" for="appstore-narrow-section">{{ $t('Section') }}</label>
				<select id="appstore-narrow-section" class="narrow-select" :value="narrowSection" @change="onNarrowSection($event.target.value)">
					<option value="discover">{{ $t('Discover') }}</option>
					<option value="all">{{ $t('All Apps') }} ({{ allAppsList.length }})</option>
					<option value="installed">{{ $t('Installed') }} ({{ installedList.length }})</option>
					<optgroup :label="$t('Categories')">
						<option v-for="cat in sidebarCategories" :key="'n-' + cat.id" :value="'cat:' + cat.name">{{ cat.name }} ({{ cat.count }})</option>
					</optgroup>
				</select>
				<button type="button" class="icon-btn" :title="$t('Custom Install')" :aria-label="$t('Custom Install')" @click="openCustomInstall">
					<i class="mdi mdi-plus" aria-hidden="true"></i>
				</button>
				<button type="button" class="icon-btn" :title="$t('App Sources')" :aria-label="$t('App Sources')" @click="showSourcesModal = true">
					<i class="mdi mdi-source-branch" aria-hidden="true"></i>
				</button>
			</nav>

			<!-- Scrollable Content Body -->
			<div class="main-body">
				<!-- Catalog couldn't be loaded: say so (it used to look like an
				     empty store / "no apps found"). -->
				<div v-if="loadError && !allAppsList.length" class="store-error" role="alert">
					<i class="mdi mdi-cloud-alert-outline store-error-icon" aria-hidden="true"></i>
					<h4 class="empty-title">{{ $t('The app catalog could not be loaded') }}</h4>
					<p class="empty-desc">{{ loadError }}</p>
					<button type="button" class="empty-action-btn" :disabled="isLoading" @click="refreshStore">{{ $t('Try again') }}</button>
				</div>

				<!-- Discover Tab: Featured Hero Carousel & Curated Categories -->
				<section v-else-if="activeTab === 'discover' && !searchQuery" class="discover-section">
					<!-- Hero Swiper Carousel -->
					<div v-if="recommendList.length > 0" class="hero-carousel-wrapper">
						<div class="hero-carousel" @mouseenter="heroPaused = true" @mouseleave="heroPaused = false" @focusin="heroPaused = true" @focusout="heroPaused = false">
							<div
								v-for="(item, idx) in featuredList"
								:key="'feat-' + item.id"
								class="hero-slide"
								:class="{ 'is-active': currentHeroIndex === idx }"
								:aria-hidden="currentHeroIndex === idx ? null : 'true'"
								:inert="currentHeroIndex === idx ? null : ''"
							>
								<div class="hero-ambient-glow"></div>

								<div class="hero-content">
									<div class="hero-badge">
										<i class="mdi mdi-star"></i>
										<span>{{ $t('Featured') }}</span>
									</div>
									<div class="hero-app-info">
										<img :src="item.icon" class="hero-app-icon" alt="" @error="onIconError" />
										<div class="hero-text-col">
											<h3 class="hero-app-title">{{ item.title }}</h3>
											<span class="hero-app-meta">{{ item.category }}<template v-if="item.author"> • {{ item.author }}</template></span>
										</div>
									</div>
									<p class="hero-app-tagline">{{ item.tagline }}</p>
									<div class="hero-actions">
										<button
											v-if="installedList.includes(item.id)"
											class="hero-action-btn is-open"
											@click="openThirdContainerByAppInfo(item)"
										>
											<i class="mdi mdi-launch"></i>
											<span>{{ $t('Open App') }}</span>
										</button>
										<template v-else>
											<button
												class="hero-action-btn is-install"
												:disabled="!isArchCompatible(item) || isAppInstalling(item.id)"
												:class="{ 'is-loading': isAppInstalling(item.id) }"
												@click="installApp(item.id, item)"
											>
												<i v-if="!isAppInstalling(item.id)" class="mdi mdi-download"></i>
												<span>{{ getInstallButtonText(item.id) }}</span>
											</button>
											<button
												class="hero-action-btn is-customize"
												:disabled="!isArchCompatible(item)"
												@click="openCustomizeForApp(item.id, item)"
												:title="$t('Customize ports, volumes, and settings before installing')"
											>
												<i class="mdi mdi-tune-variant"></i>
												<span>{{ $t('Customize') }}</span>
											</button>
										</template>
										<button type="button" class="hero-details-btn" @click="showAppDetail(item.id)">
											<span>{{ $t('Details') }}</span>
											<i class="mdi mdi-chevron-right"></i>
										</button>
									</div>
								</div>

								<button type="button" class="hero-preview-box" :aria-label="$t('{title}: details', { title: item.title })" @click="showAppDetail(item.id)">
									<img
										:src="item.thumbnail || item.screenshots[0] || item.icon"
										class="hero-preview-img"
										:alt="item.title"
										@error="onBannerError($event, item)"
									/>
								</button>
							</div>

							<button type="button" class="carousel-arrow is-prev" :title="$t('Previous')" :aria-label="$t('Previous')" @click="prevHero">
								<i class="mdi mdi-chevron-left"></i>
							</button>
							<button type="button" class="carousel-arrow is-next" :title="$t('Next')" :aria-label="$t('Next')" @click="nextHero">
								<i class="mdi mdi-chevron-right"></i>
							</button>

							<div class="carousel-dots" v-if="featuredList.length > 1">
								<button
									v-for="(_, idx) in featuredList"
									:key="'dot-' + idx"
									type="button"
									class="carousel-dot"
									:class="{ 'is-active': currentHeroIndex === idx }"
									:aria-label="$t('Show featured app {n}', { n: idx + 1 })"
									:aria-current="currentHeroIndex === idx ? 'true' : null"
									@click="currentHeroIndex = idx"
								></button>
							</div>
						</div>
					</div>

					<!-- Spotlight Picks -->
					<div v-if="recommendList.length > 0" class="section-block">
						<div class="section-header">
							<div>
								<h3 class="section-title">{{ $t('Recommended') }}</h3>
								<span class="section-subtitle">{{ $t('Popular picks from your app sources') }}</span>
							</div>
						</div>
						<div class="app-grid">
							<store-app-card
								v-for="item in recommendList.slice(0, 4)"
								:key="'spotlight-' + item.id"
								:item="item"
								:installed="installedSet.has(item.id)"
								:installing="installingMap[item.id]"
								:compatible="isArchCompatible(item)"
								:arch="arch"
								:category-icon="getCateIcon(item.category)"
								@detail="showAppDetail"
								@install="installApp"
								@customize="openCustomizeForApp"
								@open="openThirdContainerByAppInfo"
							></store-app-card>
						</div>
					</div>

					<!-- The store's largest categories (from the catalog itself, so
					     a store with other category names still gets rows). -->
					<div v-for="row in discoverRows" :key="'row-' + row.name" class="section-block">
						<div class="section-header">
							<div>
								<h3 class="section-title">{{ row.name }}</h3>
								<span class="section-subtitle">{{ $t('{n} apps', { n: row.count }) }}</span>
							</div>
							<button type="button" class="see-all-btn" @click="selectCategoryByName(row.name)">
								<span>{{ $t('See all') }}</span>
								<i class="mdi mdi-chevron-right" aria-hidden="true"></i>
							</button>
						</div>
						<div class="app-grid">
							<store-app-card
								v-for="item in row.apps"
								:key="row.name + '-' + item.id"
								:item="item"
								:installed="installedSet.has(item.id)"
								:installing="installingMap[item.id]"
								:compatible="isArchCompatible(item)"
								:arch="arch"
								:category-icon="getCateIcon(item.category)"
								@detail="showAppDetail"
								@install="installApp"
								@customize="openCustomizeForApp"
								@open="openThirdContainerByAppInfo"
							></store-app-card>
						</div>
					</div>
				</section>

				<!-- Catalog Grid (All / Category / Installed / Search) -->
				<section v-else class="catalog-section">
					<div class="catalog-header">
						<div>
							<h2 class="catalog-title">{{ currentViewTitle }}</h2>
							<p class="catalog-subtitle">{{ displayAppsList.length }} {{ $t('applications available') }}</p>
						</div>
					</div>

					<!-- Loading Skeletons -->
					<div v-if="isLoading && displayAppsList.length === 0" class="app-grid">
						<div v-for="n in 8" :key="'skel-' + n" class="app-card is-skeleton">
							<div class="skeleton-banner"></div>
							<div class="app-card-body">
								<div class="app-card-top">
									<div class="skeleton-icon"></div>
									<div class="skeleton-info">
										<div class="skeleton-line is-title"></div>
										<div class="skeleton-line is-subtitle"></div>
									</div>
								</div>
								<div class="skeleton-line is-tag"></div>
							</div>
						</div>
					</div>

					<!-- Empty State -->
					<div v-else-if="displayAppsList.length === 0" class="empty-state">
						<i class="mdi mdi-package-variant empty-icon"></i>
						<h4 class="empty-title">{{ $t('No applications found') }}</h4>
						<p class="empty-desc">{{ $t('Try searching with different keywords or select a different category.') }}</p>
						<button v-if="searchQuery" class="empty-action-btn" @click="clearSearch">
							{{ $t('Clear Search') }}
						</button>
					</div>

					<!-- App Cards Grid -->
					<div v-else class="app-grid">
						<store-app-card
							v-for="item in displayAppsList"
							:key="item.id"
							:item="item"
							:installed="installedSet.has(item.id)"
							:installing="installingMap[item.id]"
							:compatible="isArchCompatible(item)"
							:arch="arch"
							:category-icon="getCateIcon(item.category)"
							@detail="showAppDetail"
							@install="installApp"
							@customize="openCustomizeForApp"
							@open="openThirdContainerByAppInfo"
						></store-app-card>
					</div>
				</section>
			</div>
		</main>

		<!-- Mode 2: Custom Modern Container Studio & Installer View -->
		<main v-else-if="viewMode === 'installer'" class="appstore-installer">
			<!-- Header Toolbar -->
			<header class="installer-header">
				<div class="installer-header-left">
					<button class="back-to-store-btn" @click="viewMode = 'store'">
						<i class="mdi mdi-arrow-left"></i>
						<span>{{ $t('Back to Store') }}</span>
					</button>
					<div class="installer-app-badge">
						<img :src="formState.icon || defaultAppIcon" class="installer-app-icon" :alt="formState.title || formState.appName || ''" @error="onFormIconError" />
						<div class="installer-app-info">
							<h3 class="installer-app-title">{{ formState.title || formState.appName || $t('Custom Container') }}</h3>
							<span class="installer-app-sub">{{ formState.image || 'docker:image' }}</span>
						</div>
					</div>
				</div>

				<!-- Center Mode Switcher -->
				<div class="installer-header-center">
					<div class="view-switch-pills">
						<button
							class="view-pill-btn"
							:class="{ 'is-active': installerTab === 'form' }"
							@click="switchInstallerTab('form')"
						>
							<i class="mdi mdi-form-select"></i>
							<span>{{ $t('Visual Editor') }}</span>
						</button>
						<button
							class="view-pill-btn"
							:class="{ 'is-active': installerTab === 'yaml' }"
							@click="switchInstallerTab('yaml')"
						>
							<i class="mdi mdi-code-braces"></i>
							<span>{{ $t('Compose YAML') }}</span>
						</button>
					</div>
				</div>

				<!-- Right Action Tools -->
				<div class="installer-header-right">
					<button class="tool-action-btn" :title="$t('Import Docker CLI or Compose YAML')" @click="openImportModal">
						<i class="mdi mdi-import"></i>
						<span>{{ $t('Import') }}</span>
					</button>
					<button class="tool-action-btn" :title="$t('Export Compose File')" @click="exportYAML">
						<i class="mdi mdi-export-variant"></i>
						<span>{{ $t('Export') }}</span>
					</button>
				</div>
			</header>

			<!-- Installer Body -->
			<div class="installer-body">
				<!-- Tab 1: Visual Form Editor -->
				<div v-if="installerTab === 'form'" class="installer-form-layout">
					<!-- Section 1: Identity & Image -->
					<div class="installer-card">
						<div class="card-title-row">
							<div class="card-icon-pill is-blue"><i class="mdi mdi-docker"></i></div>
							<div>
								<h4 class="card-heading">{{ $t('General & Image Configuration') }}</h4>
								<span class="card-caption">{{ $t('Define container name, image source, and identity metadata') }}</span>
							</div>
						</div>

						<div class="form-grid-2">
							<div class="form-group">
								<label class="form-label">{{ $t('Display Name') }} <span class="req">*</span></label>
								<input :aria-label="$t('Display Name')" v-model="formState.title" type="text" class="form-input" :placeholder="$t('e.g., Nextcloud')" />
							</div>
							<div class="form-group">
								<label class="form-label">{{ $t('Container Name') }} <span class="req">*</span></label>
								<input :aria-label="$t('Container Name')" v-model="formState.containerName" type="text" class="form-input" :placeholder="$t('e.g., nextcloud')" />
							</div>
						</div>

						<div class="form-grid-2 mt-3">
							<div class="form-group">
								<label class="form-label">{{ $t('Docker Image') }} <span class="req">*</span></label>
								<div class="input-with-tags">
									<input :aria-label="$t('Docker Image')" v-model="formState.image" type="text" class="form-input" :placeholder="$t('e.g., nextcloud:latest')" />
								</div>
								<div class="quick-tags">
									<span class="quick-tag-label">{{ $t('Quick Tags:') }}</span>
									<button type="button" class="quick-tag-btn" @click="appendImageTag('latest')">:latest</button>
									<button type="button" class="quick-tag-btn" @click="appendImageTag('alpine')">:alpine</button>
									<button type="button" class="quick-tag-btn" @click="appendImageTag('stable')">:stable</button>
								</div>
							</div>

							<div class="form-group">
								<label class="form-label">{{ $t('Category') }}</label>
								<select :aria-label="$t('Category')" v-model="formState.category" class="form-select">
									<option value="Productivity">{{ $t('Productivity') }}</option>
									<option value="Media">{{ $t('Media & Streaming') }}</option>
									<option value="AI">{{ $t('AI & LLMs') }}</option>
									<option value="Developer">{{ $t('Developer & DevOps') }}</option>
									<option value="Networking">{{ $t('Networking & Privacy') }}</option>
									<option value="Home">{{ $t('Home Automation') }}</option>
									<option value="Finance">{{ $t('Finance & Crypto') }}</option>
									<option value="Social">{{ $t('Social & Chat') }}</option>
									<option value="Utilities">{{ $t('Utilities') }}</option>
									<option value="Others">{{ $t('Others') }}</option>
								</select>
							</div>
						</div>

						<div class="form-group mt-3">
							<label class="form-label">{{ $t('Icon URL') }}</label>
							<div class="icon-input-row">
								<img :src="formState.icon || defaultAppIcon" class="icon-preview-thumb" alt="" @error="onFormIconError" />
								<input :aria-label="$t('Icon URL')" v-model="formState.icon" type="text" class="form-input" :placeholder="$t('https://icon.casaos.io/main/all/app.png')" />
							</div>
						</div>
					</div>

					<!-- Section 2: Web UI & Access -->
					<div class="installer-card">
						<div class="card-title-row">
							<div class="card-icon-pill is-emerald"><i class="mdi mdi-web"></i></div>
							<div class="card-title-left">
								<h4 class="card-heading">{{ $t('Web UI & Dashboard Access') }}</h4>
								<span class="card-caption">{{ $t('Configure the web portal and direct access port in NivaroOS') }}</span>
							</div>
							<div class="card-toggle-wrap">
								<b-switch v-model="formState.webUI.enabled" type="is-success">{{ $t('Enable Web UI') }}</b-switch>
							</div>
						</div>

						<div v-if="formState.webUI.enabled" class="form-grid-3 mt-3">
							<div class="form-group">
								<label class="form-label">{{ $t('Protocol') }}</label>
								<select :aria-label="$t('Protocol')" v-model="formState.webUI.scheme" class="form-select">
									<option value="http">http://</option>
									<option value="https">https://</option>
								</select>
							</div>
							<div class="form-group">
								<label class="form-label">{{ $t('Port') }}</label>
								<input :aria-label="$t('Port')" v-model="formState.webUI.port" type="text" class="form-input" :placeholder="$t('e.g., 80 or 8080')" />
							</div>
							<div class="form-group">
								<label class="form-label">{{ $t('Index Path [Optional]') }}</label>
								<input :aria-label="$t('Index Path [Optional]')" v-model="formState.webUI.index" type="text" class="form-input" :placeholder="$t('/index.html or /login')" />
							</div>
						</div>
					</div>

					<!-- Section 3: Port Mappings -->
					<div class="installer-card">
						<div class="card-title-row">
							<div class="card-icon-pill is-indigo"><i class="mdi mdi-lan-connect"></i></div>
							<div>
								<h4 class="card-heading">{{ $t('Port Mappings') }}</h4>
								<span class="card-caption">{{ $t('Expose internal container services to your host network') }}</span>
							</div>
						</div>

						<div class="dynamic-list mt-3">
							<div v-for="(p, pidx) in formState.ports" :key="'port-' + pidx" class="dynamic-row">
								<div class="dynamic-field">
									<span class="field-mini-label">{{ $t('Host Port') }}</span>
									<input :aria-label="$t('Host Port')" v-model="p.host" type="text" class="form-input is-sm" placeholder="8080" />
								</div>
								<span class="row-arrow">➔</span>
								<div class="dynamic-field">
									<span class="field-mini-label">{{ $t('Container Port') }}</span>
									<input :aria-label="$t('Container Port')" v-model="p.container" type="text" class="form-input is-sm" placeholder="80" />
								</div>
								<div class="dynamic-field is-protocol">
									<span class="field-mini-label">{{ $t('Protocol') }}</span>
									<select :aria-label="$t('Protocol')" v-model="p.protocol" class="form-select is-sm">
										<option value="TCP">TCP</option>
										<option value="UDP">UDP</option>
									</select>
								</div>
								<button type="button" class="delete-row-btn" :aria-label="$t('Remove row')" :title="$t('Remove row')" @click="removePort(pidx)">
									<i class="mdi mdi-delete-outline"></i>
								</button>
							</div>

							<button type="button" class="add-row-btn" @click="addPort">
								<i class="mdi mdi-plus"></i>
								<span>{{ $t('Add Port Mapping') }}</span>
							</button>
						</div>
					</div>

					<!-- Section 4: Volumes & Storage -->
					<div class="installer-card">
						<div class="card-title-row">
							<div class="card-icon-pill is-amber"><i class="mdi mdi-folder-multiple-outline"></i></div>
							<div>
								<h4 class="card-heading">{{ $t('Storage & Volume Mounts') }}</h4>
								<span class="card-caption">{{ $t('Bind local host directories for configuration persistence and media') }}</span>
							</div>
						</div>

						<div class="dynamic-list mt-3">
							<div v-for="(v, vidx) in formState.volumes" :key="'vol-' + vidx" class="dynamic-row">
								<div class="dynamic-field is-wide">
									<span class="field-mini-label">{{ $t('Host Path') }}</span>
									<input :aria-label="$t('Host Path')" v-model="v.host" type="text" class="form-input is-sm" placeholder="/DATA/AppData/app/config" />
								</div>
								<span class="row-arrow">➔</span>
								<div class="dynamic-field is-wide">
									<span class="field-mini-label">{{ $t('Container Path') }}</span>
									<input :aria-label="$t('Container Path')" v-model="v.container" type="text" class="form-input is-sm" placeholder="/config" />
								</div>
								<div class="dynamic-field is-mode">
									<span class="field-mini-label">{{ $t('Mode') }}</span>
									<select :aria-label="$t('Mode')" v-model="v.mode" class="form-select is-sm">
										<option value="rw">Read / Write (rw)</option>
										<option value="ro">Read-Only (ro)</option>
									</select>
								</div>
								<button type="button" class="delete-row-btn" :aria-label="$t('Remove row')" :title="$t('Remove row')" @click="removeVolume(vidx)">
									<i class="mdi mdi-delete-outline"></i>
								</button>
							</div>

							<button type="button" class="add-row-btn" @click="addVolume">
								<i class="mdi mdi-plus"></i>
								<span>{{ $t('Add Volume Mount') }}</span>
							</button>
						</div>
					</div>

					<!-- Section 5: Environment Variables -->
					<div class="installer-card">
						<div class="card-title-row">
							<div class="card-icon-pill is-purple"><i class="mdi mdi-code-tags"></i></div>
							<div>
								<h4 class="card-heading">{{ $t('Environment Variables') }}</h4>
								<span class="card-caption">{{ $t('Inject configuration keys, passwords, and timezone values') }}</span>
							</div>
						</div>

						<div class="dynamic-list mt-3">
							<div v-for="(e, eidx) in formState.envs" :key="'env-' + eidx" class="dynamic-row">
								<div class="dynamic-field is-wide">
									<span class="field-mini-label">{{ $t('Key / Name') }}</span>
									<input :aria-label="$t('Key / Name')" v-model="e.key" type="text" class="form-input is-sm is-mono" placeholder="TZ" />
								</div>
								<span class="row-arrow">=</span>
								<div class="dynamic-field is-wide">
									<span class="field-mini-label">{{ $t('Value') }}</span>
									<input :aria-label="$t('Value')" v-model="e.value" type="text" class="form-input is-sm is-mono" placeholder="UTC" />
								</div>
								<button type="button" class="delete-row-btn" :aria-label="$t('Remove row')" :title="$t('Remove row')" @click="removeEnv(eidx)">
									<i class="mdi mdi-delete-outline"></i>
								</button>
							</div>

							<button type="button" class="add-row-btn" @click="addEnv">
								<i class="mdi mdi-plus"></i>
								<span>{{ $t('Add Environment Variable') }}</span>
							</button>
						</div>
					</div>

					<!-- Section 6: Advanced & Hardware -->
					<div class="installer-card">
						<div class="card-title-row is-clickable" @click="showAdvanced = !showAdvanced">
							<div class="card-icon-pill is-slate"><i class="mdi mdi-cog-outline"></i></div>
							<div class="card-title-left">
								<h4 class="card-heading">{{ $t('Advanced & Hardware Configuration') }}</h4>
								<span class="card-caption">{{ $t('Network drivers, restart policy, memory limits, and GPU / hardware devices') }}</span>
							</div>
							<button type="button" class="accordion-toggle-btn" :aria-label="$t('Advanced settings')" :aria-expanded="showAdvanced ? 'true' : 'false'" @click.stop="showAdvanced = !showAdvanced">
								<i :class="'mdi ' + (showAdvanced ? 'mdi-chevron-up' : 'mdi-chevron-down')"></i>
							</button>
						</div>

						<div v-if="showAdvanced" class="advanced-body mt-4">
							<div class="form-grid-3">
								<div class="form-group">
									<label class="form-label">{{ $t('Network Driver') }}</label>
									<select :aria-label="$t('Network Driver')" v-model="formState.network" class="form-select">
										<option value="bridge">bridge (Isolated NAT)</option>
										<option value="host">host (Direct Host Networking)</option>
										<option v-for="net in networkOptions" :key="net" :value="net">{{ net }}</option>
										<option v-if="formState.network && !['bridge', 'host'].includes(formState.network) && !networkOptions.includes(formState.network)" :value="formState.network">{{ formState.network }}</option>
									</select>
								</div>

								<div class="form-group">
									<label class="form-label">{{ $t('Restart Policy') }}</label>
									<select :aria-label="$t('Restart Policy')" v-model="formState.restart" class="form-select">
										<option value="unless-stopped">unless-stopped (Recommended)</option>
										<option value="always">always</option>
										<option value="on-failure">on-failure</option>
										<option value="no">no</option>
									</select>
								</div>

								<div class="form-group">
									<label class="form-label">{{ $t('Memory Limit (MB)') }}</label>
									<input :aria-label="$t('Memory Limit (MB)')" v-model="formState.memoryLimit" type="text" class="form-input" :placeholder="$t('0 = no limit')" />
								</div>
							</div>

							<div class="form-group mt-3">
								<label class="form-label">{{ $t('Privileged Container') }}</label>
								<div class="switch-row">
									<b-switch v-model="formState.privileged" type="is-danger">{{ $t('Grant container full root access to host devices & kernel') }}</b-switch>
								</div>
							</div>

							<div class="form-group mt-3">
								<label class="form-label">{{ $t('Container Command / Entrypoint') }}</label>
								<input :aria-label="$t('Container Command / Entrypoint')" v-model="formState.command" type="text" class="form-input is-mono" placeholder="e.g., sh -c 'npm start'" />
							</div>

							<!-- Devices Section -->
							<div class="mt-4">
								<label class="form-label">{{ $t('Hardware Device Passthrough') }}</label>
								<div class="dynamic-list">
									<div v-for="(dev, didx) in formState.devices" :key="'dev-' + didx" class="dynamic-row">
										<div class="dynamic-field is-wide">
											<span class="field-mini-label">{{ $t('Host Device') }}</span>
											<input :aria-label="$t('Host Device')" v-model="dev.host" type="text" class="form-input is-sm is-mono" placeholder="/dev/dri" />
										</div>
										<span class="row-arrow">➔</span>
										<div class="dynamic-field is-wide">
											<span class="field-mini-label">{{ $t('Container Device') }}</span>
											<input :aria-label="$t('Container Device')" v-model="dev.container" type="text" class="form-input is-sm is-mono" placeholder="/dev/dri" />
										</div>
										<button type="button" class="delete-row-btn" :aria-label="$t('Remove row')" :title="$t('Remove row')" @click="removeDevice(didx)">
											<i class="mdi mdi-delete-outline"></i>
										</button>
									</div>
									<button type="button" class="add-row-btn" @click="addDevice">
										<i class="mdi mdi-plus"></i>
										<span>{{ $t('Add Device Passthrough') }}</span>
									</button>
								</div>
							</div>
						</div>
					</div>
				</div>

				<!-- Tab 2: Direct YAML Editor -->
				<div v-else class="yaml-studio-wrapper">
					<div class="yaml-studio-bar">
						<div class="yaml-stat">
							<i class="mdi mdi-file-document-outline"></i>
							<span>docker-compose.yml</span>
						</div>
						<button class="copy-yaml-btn" @click="copyYAML">
							<i class="mdi mdi-content-copy"></i>
							<span>{{ $t('Copy Code') }}</span>
						</button>
					</div>
					<textarea
						v-model="customComposeYaml"
						class="yaml-studio-textarea"
						spellcheck="false"
						placeholder="version: '3.8'&#10;services:&#10;  app:&#10;    image: nginx:alpine&#10;    ports:&#10;      - '8080:80'"
					></textarea>
				</div>
			</div>

			<!-- Installer Bottom Footer -->
			<div v-if="installerTab === 'form' && formErrors.length" class="installer-errors" role="alert">
				<i class="mdi mdi-alert-circle-outline" aria-hidden="true"></i>
				<ul>
					<li v-for="err in formErrors" :key="err">{{ err }}</li>
				</ul>
			</div>
			<footer class="installer-footer">
				<div class="installer-footer-info">
					<i class="mdi mdi-information-outline"></i>
					<span>{{ formState.isEditing ? $t('Saving recreates the container with these settings. Its data folders are kept.') : $t('The app is installed as a Docker Compose app and appears on your desktop.') }}</span>
				</div>
				<div class="installer-footer-btns">
					<button class="installer-cancel-btn" @click="viewMode = 'store'">
						{{ $t('Cancel') }}
					</button>
					<button
						class="installer-deploy-btn"
						:class="{ 'is-loading': isDeployingCustom }"
						:disabled="!isFormValid || isDeployingCustom"
						@click="installFromInstaller"
					>
						<i v-if="!isDeployingCustom" class="mdi mdi-check-circle-outline"></i>
						<span v-if="!isDeployingCustom">{{ formState.isEditing ? $t('Save & Apply Settings') : $t('Install Container') }}</span>
						<span v-else>{{ formState.isEditing ? $t('Applying Settings...') : $t('Deploying Container...') }}</span>
					</button>
				</div>
			</footer>
		</main>

		<!-- App details, screenshots and App Sources are real desktop
		     windows (they were overlays covering the whole store). -->
		<settings-overlay
			:active="!!selectedAppDetail"
			:title="selectedAppDetail ? i18n(selectedAppDetail.title) : ''"
			width="46rem"
			body-class="appstore-detail-body"
			@close="closeAppDetail"
		>
			<div v-if="selectedAppDetail" class="detail-window">
				<div class="detail-hero">
					<img :src="selectedAppDetail.icon || defaultAppIcon" class="detail-icon" alt="" @error="onIconError" />
					<div class="detail-hero-info">
						<h2 class="detail-title">{{ i18n(selectedAppDetail.title) }}</h2>
						<p class="detail-tagline">{{ i18n(selectedAppDetail.tagline) }}</p>
						<div class="detail-meta-row">
							<span v-if="selectedAppDetail.category" class="detail-pill">{{ selectedAppDetail.category }}</span>
							<span v-if="selectedAppDetail.version" class="detail-pill is-subtle">v{{ selectedAppDetail.version }}</span>
							<span v-if="selectedAppDetail.developer || selectedAppDetail.author" class="detail-pill is-subtle">{{ selectedAppDetail.developer || selectedAppDetail.author }}</span>
							<span v-if="!isArchCompatible(selectedAppDetail)" class="detail-pill is-danger">{{ $t('Not built for this server\'s CPU ({arch})', { arch: arch || '?' }) }}</span>
						</div>
						<div class="detail-actions">
							<button
								v-if="installedSet.has(selectedAppDetail.id)"
								type="button"
								class="detail-action-btn is-open"
								@click="openThirdContainerByAppInfo(selectedAppDetail)"
							>
								<i class="mdi mdi-launch" aria-hidden="true"></i>
								<span>{{ $t('Open App') }}</span>
							</button>
							<template v-else>
								<button
									type="button"
									class="detail-action-btn is-install"
									:disabled="!isArchCompatible(selectedAppDetail) || isAppInstalling(selectedAppDetail.id)"
									:class="{ 'is-loading': isAppInstalling(selectedAppDetail.id) }"
									@click="installApp(selectedAppDetail.id, selectedAppDetail)"
								>
									<i v-if="!isAppInstalling(selectedAppDetail.id)" class="mdi mdi-download" aria-hidden="true"></i>
									<span>{{ getInstallButtonText(selectedAppDetail.id) }}</span>
								</button>
								<button
									type="button"
									class="detail-action-btn is-customize"
									:disabled="!isArchCompatible(selectedAppDetail) || isAppInstalling(selectedAppDetail.id)"
									@click="openCustomizeForApp(selectedAppDetail.id, selectedAppDetail)"
								>
									<i class="mdi mdi-tune-variant" aria-hidden="true"></i>
									<span>{{ $t('Customize & Install') }}</span>
								</button>
							</template>
						</div>
					</div>
				</div>

				<div v-if="detailScreenshots.length > 0" class="detail-section">
					<h4 class="detail-section-title">{{ $t('Screenshots') }}</h4>
					<div class="screenshots-gallery" tabindex="0" :aria-label="$t('Screenshots')">
						<button
							v-for="(img, sidx) in detailScreenshots"
							:key="'screen-' + sidx"
							type="button"
							class="screenshot-item"
							:aria-label="$t('Enlarge screenshot {n}', { n: sidx + 1 })"
							@click="activeLightboxImage = img"
						>
							<img :src="img" alt="" loading="lazy" />
							<span class="screenshot-hover-overlay" aria-hidden="true">
								<i class="mdi mdi-magnify-plus-outline"></i>
							</span>
						</button>
					</div>
				</div>

				<div class="detail-section">
					<h4 class="detail-section-title">{{ $t('About this app') }}</h4>
					<div class="detail-description">
						<p class="description-text">{{ i18n(selectedAppDetail.description) || i18n(selectedAppDetail.tagline) }}</p>
					</div>
				</div>

				<!-- Only facts the store actually provides (it used to show
				     made-up fallbacks like "256 MB" and "Community"). -->
				<div v-if="detailSpecs.length" class="detail-section">
					<h4 class="detail-section-title">{{ $t('Details') }}</h4>
					<div class="specs-grid">
						<div v-for="spec in detailSpecs" :key="spec.label" class="spec-card">
							<span class="spec-label">{{ spec.label }}</span>
							<span class="spec-value">{{ spec.value }}</span>
						</div>
					</div>
				</div>
			</div>
		</settings-overlay>

		<settings-overlay :active="!!activeLightboxImage" :title="$t('Screenshot')" width="60rem" @close="activeLightboxImage = null">
			<img v-if="activeLightboxImage" :src="activeLightboxImage" class="lightbox-img" :alt="$t('Screenshot')" />
		</settings-overlay>

		<settings-overlay :active="showSourcesModal" :title="$t('App Sources')" width="34rem" @close="showSourcesModal = false">
			<p class="sources-intro">{{ $t('Each source is a .zip catalog of app definitions. Apps from every source appear together in the store.') }}</p>
			<form class="add-source-box" @submit.prevent="addStoreSource">
				<label class="sr-only" for="appstore-source-url">{{ $t('Source address') }}</label>
				<input
					id="appstore-source-url"
					v-model="newSourceUrl"
					type="url"
					class="source-input"
					placeholder="https://example.com/store/main.zip"
					:disabled="!!addingSource"
				/>
				<button type="submit" class="add-source-btn" :disabled="!newSourceUrl.trim() || !!addingSource">
					<i v-if="addingSource" class="mdi mdi-loading mdi-spin" aria-hidden="true"></i>
					{{ addingSource ? $t('Adding...') : $t('Add app source') }}
				</button>
			</form>
			<p v-if="addingSource" class="sources-hint" role="status">{{ $t('Downloading and checking the catalog - this can take a minute.') }}</p>
			<div class="sources-list">
				<div v-for="src in storeSourcesList" :key="'src-' + (src.id !== undefined ? src.id : src.url)" class="source-row">
					<i class="mdi mdi-source-branch source-row-icon" aria-hidden="true"></i>
					<div class="source-info">
						<span class="source-name" :title="src.url || src">{{ sourceLabel(src) }}</span>
						<span class="source-url">{{ src.url || src }}</span>
					</div>
					<button
						type="button"
						class="delete-source-btn"
						:disabled="removingSourceId === src.id"
						:title="$t('Remove')"
						:aria-label="$t('Remove {url}', { url: src.url || src })"
						@click="removeStoreSource(src)"
					>
						<i :class="removingSourceId === src.id ? 'mdi mdi-loading mdi-spin' : 'mdi mdi-delete-outline'" aria-hidden="true"></i>
					</button>
				</div>
				<p v-if="!storeSourcesList.length" class="sources-hint">{{ $t('No app sources yet.') }}</p>
			</div>
		</settings-overlay>
	</div>
</template>

<script>
import appStoreIcon from '@/assets/img/app-icons/appstore.png'
import defaultAppIcon from '@/assets/img/app-icons/default.svg'
import business_OpenThirdApp from '@/mixins/app/Business_OpenThirdApp'
import business_ShowNewAppTag from '@/mixins/app/Business_ShowNewAppTag'
import { ice_i18n } from '@/mixins/base/common-i18n'
import debounce from 'lodash/debounce'
import YAML from 'yaml'
import FileSaver from 'file-saver'
import copy from 'clipboard-copy'
import { escapeHtml } from '@/utils/escapeHtml'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import SettingsOverlay from '@/apps/settings/SettingsOverlay.vue'
import StoreAppCard from './StoreAppCard.vue'
import { applyFormToDoc, docToFormState, parsePortSpec } from './composeForm'

const ARCH_MAP = {
	x86_64: 'amd64',
	aarch64: 'arm64',
	armv7l: 'arm',
	armhf: 'arm'
}

// Publishers treated as "official" by the Publisher filter.
const OFFICIAL_AUTHORS = ['NivaroOS', 'CasaOS', 'IceWhale', 'ZimaOS Team', 'Official']

const GRADIENTS = [
	'linear-gradient(135deg, #1e293b 0%, #0f172a 100%)',
	'linear-gradient(135deg, #1e3a8a 0%, #0f172a 100%)',
	'linear-gradient(135deg, #064e3b 0%, #022c22 100%)',
	'linear-gradient(135deg, #3b0764 0%, #1e1b4b 100%)',
	'linear-gradient(135deg, #4c0519 0%, #1e1b4b 100%)',
	'linear-gradient(135deg, #431407 0%, #1e293b 100%)'
]

function createDefaultFormState() {
	return {
		isEditing: false,
		appName: 'custom-app',
		mainService: 'app',
		title: 'Custom App',
		icon: 'https://icon.casaos.io/main/all/default.png',
		category: 'Others',
		tagline: 'Custom container application',
		description: 'Custom Docker Container deployed via NivaroOS Studio',
		image: 'nginx:latest',
		containerName: 'custom-app',
		webUI: {
			enabled: true,
			scheme: 'http',
			port: '8080',
			index: ''
		},
		ports: [
			{ host: '8080', container: '80', protocol: 'TCP' }
		],
		volumes: [
			{ host: '/DATA/AppData/custom-app', container: '/data', mode: 'rw' }
		],
		envs: [
			{ key: 'TZ', value: 'UTC' }
		],
		devices: [],
		network: 'bridge',
		restart: 'unless-stopped',
		privileged: false,
		command: '',
		memoryLimit: ''
	}
}

export default {
	name: 'AppStoreApp',
	components: { StoreAppCard, SettingsOverlay },
	mixins: [business_OpenThirdApp, business_ShowNewAppTag, confirmWindowMixin],
	props: {
		// A store app id (compose name) to show right away.
		storeId: {
			type: [String, Number],
			default: ''
		},
		// Compose YAML to open in the custom installer (Import to NivaroOS on
		// a plain container sends its exported compose).
		initialComposeYaml: {
			type: String,
			default: ''
		},
		importContainerName: {
			type: String,
			default: ''
		},
		// Stamped by OPEN_WINDOW when an already-open store window is asked
		// again (e.g. "Setting" on another app): the props above changed, but
		// mounted() doesn't run again, so a watcher acts on it.
		requestedAt: {
			type: Number,
			default: 0
		},
		initialMode: {
			type: String,
			default: ''
		},
		initialAppName: {
			type: String,
			default: ''
		}
	},
	data() {
		return {
			appStoreIcon,
			defaultAppIcon,
			viewMode: 'store',
			installerTab: 'form',
			showAdvanced: false,
			activeTab: 'discover',
			isLoading: false,
			searchQuery: '',
			debouncedSearch: '',
			cateMenu: [],
			currentCate: { id: 0, name: 'All', count: 0 },
			authorMenu: [
				{ name: 'All' },
				{ name: 'Official' },
				{ name: 'Community' }
			],
			currentAuthor: { name: 'All' },
			// "Popularity"/"Newest" did nothing (no such data in the catalog).
			sortMenu: [
				{ name: 'Recommended' },
				{ name: 'Name (A-Z)' },
				{ name: 'Name (Z-A)' }
			],
			currentSort: { name: 'Recommended' },
			allAppsList: [],
			recommendList: [],
			installedList: [],
			installingMap: {}, // { appName: progressNumber }
			currentHeroIndex: 0,
			heroTimer: null,
			selectedAppDetail: null,
			activeLightboxImage: null,
			formState: createDefaultFormState(),
			customComposeYaml: '',
			isDeployingCustom: false,
			showSourcesModal: false,
			newSourceUrl: '',
			storeSourcesList: [],
			networks: [],
			width: 1040,
			resizeObserver: null,
			loadError: '',
			detailLoadingId: '',
			addingSource: false,
			removingSourceId: null,
			yamlError: '',
			serverArch: '',
			heroPaused: false
		}
	},
	computed: {
		isCompact() {
			return this.width < 768
		},
		isNarrow() {
			return this.width < 540
		},
		installedSet() {
			return new Set(this.installedList)
		},
		// The backend adds an "All" (id 0) category - the sidebar already has
		// "All Apps", so it was listed twice.
		narrowSection() {
			return this.activeTab === 'category' ? 'cat:' + this.currentCate.name : this.activeTab
		},
		sidebarCategories() {
			return this.cateMenu.filter((c) => c.name !== 'All' && c.id !== 0)
		},
		// Discover rows: the three biggest categories of this catalog.
		discoverRows() {
			return [...this.sidebarCategories]
				.sort((a, b) => b.count - a.count)
				.slice(0, 3)
				.map((c) => ({ name: c.name, count: c.count, apps: this.allAppsList.filter((item) => item.category === c.name).slice(0, 6) }))
				.filter((r) => r.apps.length)
		},
		formErrors() {
			const f = this.formState
			const errs = []
			if (this.installerTab !== 'form') return errs
			if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(f.containerName || '')) errs.push(this.$t('Container name: letters, numbers, - _ . only, starting with a letter or number.'))
			const seen = new Set()
			for (const p of f.ports || []) {
				for (const [label, v] of [['host', p.host], ['container', p.container]]) {
					if (v === '' || v === undefined) continue
					if (!/^\d+(-\d+)?$/.test(String(v)) || String(v).split('-').some((n) => +n < 1 || +n > 65535)) errs.push(this.$t('Port {port} is not valid (1-65535).', { port: v }))
					if (label === 'host' && v) {
						const k = `${v}/${p.protocol}`
						if (seen.has(k)) errs.push(this.$t('Host port {port} is used twice.', { port: v }))
						seen.add(k)
					}
				}
			}
			for (const e of f.envs || []) {
				if (e.key && !/^[A-Za-z_][A-Za-z0-9_.]*$/.test(e.key)) errs.push(this.$t('Environment variable name "{key}" is not valid.', { key: e.key }))
			}
			for (const v of f.volumes || []) {
				if (v.container && !String(v.container).startsWith('/')) errs.push(this.$t('Container path {path} must start with /.', { path: v.container }))
			}
			return [...new Set(errs)]
		},
		totalAppCount() {
			return this.allAppsList.length
		},
		networkOptions() {
			return (this.networks || []).map(n => n.name).filter(n => n && n !== 'bridge' && n !== 'host')
		},
		isFormValid() {
			if (this.installerTab === 'yaml') {
				return Boolean(this.customComposeYaml && this.customComposeYaml.trim())
			}
			return Boolean(this.formState.title && this.formState.image && this.formState.containerName) && this.formErrors.length === 0
		},
		// The server's CPU architecture, when known. hardwareInfo comes from
		// /sys/utilization, which has no arch - it used to default to amd64,
		// so every arm64-only app showed "incompatible" on an arm64 box.
		// Unknown means "don't second-guess": the backend already filters
		// the catalog for this architecture.
		arch() {
			const rawArch = this.$store.state.hardwareInfo?.cpu?.arch || this.serverArch || ''
			return ARCH_MAP[rawArch] || rawArch
		},
		featuredList() {
			return this.recommendList.slice(0, 6)
		},
		currentViewTitle() {
			if (this.searchQuery) return `${this.$t('Search Results for')} "${this.searchQuery}"`
			if (this.activeTab === 'category') return this.currentCate.name
			if (this.activeTab === 'installed') return this.$t('Installed Apps')
			return this.$t('All Applications')
		},
		displayAppsList() {
			let list = [...this.allAppsList]

			if (this.activeTab === 'installed') {
				list = list.filter(item => this.installedList.includes(item.id))
			} else if (this.activeTab === 'category' && this.currentCate.name !== 'All') {
				list = list.filter(item => item.category === this.currentCate.name)
			}

			if (this.currentAuthor.name === 'Official') {
				list = list.filter(item => OFFICIAL_AUTHORS.includes(item.author))
			} else if (this.currentAuthor.name === 'Community') {
				list = list.filter(item => !OFFICIAL_AUTHORS.includes(item.author))
			}

			if (this.debouncedSearch) {
				const words = this.debouncedSearch.toLowerCase().split(/\s+/).filter(Boolean)
				list = list.filter(item => {
					const hay = [item.title, item.tagline, item.category, item.author, item.id].join(' ').toLowerCase()
					return words.every((w) => hay.includes(w))
				})
			}

			if (this.currentSort.name === 'Name (A-Z)') {
				list.sort((a, b) => (a.title || '').localeCompare(b.title || ''))
			} else if (this.currentSort.name === 'Name (Z-A)') {
				list.sort((a, b) => (b.title || '').localeCompare(a.title || ''))
			} else {
				// Recommended first, then by name.
				const rec = new Set(this.recommendList.map((r) => r.id))
				list.sort((a, b) => (rec.has(b.id) - rec.has(a.id)) || (a.title || '').localeCompare(b.title || ''))
			}

			return list
		},
		detailSpecs() {
			const d = this.selectedAppDetail
			if (!d) return []
			const out = []
			if (d.category) out.push({ label: this.$t('Category'), value: d.category })
			if (d.min_memory) out.push({ label: this.$t('Memory required'), value: d.min_memory })
			if (d.architectures && d.architectures.length) out.push({ label: this.$t('Architectures'), value: d.architectures.join(', ') })
			if (d.developer || d.author) out.push({ label: this.$t('Developer'), value: d.developer || d.author })
			if (d.version) out.push({ label: this.$t('Version'), value: d.version })
			return out
		},
		detailScreenshots() {
			if (!this.selectedAppDetail) return []
			const links = this.selectedAppDetail.screenshot_link || []
			return Array.isArray(links) ? links.filter(Boolean) : [links]
		}
	},
	// No deep formState watcher any more: it regenerated the YAML from the
	// form on every keystroke, throwing away everything the form doesn't
	// model. The YAML is now produced from the loaded document + the form's
	// edits only when it's needed (YAML tab, export, deploy).
	watch: {
		requestedAt() {
			this.handleDeepLink()
		},
		activeTab() {
			this.startHeroAutoplay()
		},
		viewMode() {
			this.startHeroAutoplay()
		}
	},
	created() {
		this.onSearchInput = debounce(() => {
			this.debouncedSearch = this.searchQuery
			if (this.searchQuery && this.activeTab === 'discover') {
				this.searchFromTab = 'discover'
				this.activeTab = 'all'
			}
			if (!this.searchQuery && this.searchFromTab) this.clearSearch()
		}, 250)
	},
	async mounted() {
		// Only the breakpoints matter; assigning the raw width on every
		// resize frame re-rendered the whole store.
		this.resizeObserver = new ResizeObserver(entries => {
			if (!entries || !entries[0]) return
			const w = entries[0].contentRect.width
			const bucket = w < 540 ? 500 : w < 768 ? 700 : 1040
			if (bucket !== this.width) this.width = bucket
		})
		this.resizeObserver.observe(this.$el)

		await Promise.all([
			this.initStore(),
			this.fetchNetworks(),
			this.fetchServerArch()
		])

		this.startHeroAutoplay()
		this.handleDeepLink()
		document.addEventListener('visibilitychange', this.startHeroAutoplay)
	},
	beforeDestroy() {
		this.destroyed = true
		if (this.resizeObserver) this.resizeObserver.disconnect()
		if (this.heroTimer) clearInterval(this.heroTimer)
		if (this.onSearchInput && this.onSearchInput.cancel) this.onSearchInput.cancel()
		clearTimeout(this.refreshListTimer)
		document.removeEventListener('visibilitychange', this.startHeroAutoplay)
	},
	methods: {
		i18n(text) {
			return ice_i18n(text)
		},
		isAppInstalling(id) {
			return this.installingMap[id] !== undefined
		},
		getInstallButtonText(id) {
			const p = this.installingMap[id]
			if (p !== undefined) {
				return p > 0 ? `${p}%` : this.$t('Installing...')
			}
			return this.$t('Install')
		},
		getCateIcon(name) {
			const n = (name || '').toLowerCase().trim()
			if (n === 'all') return 'view-grid-outline'
			// Whole words: includes('it') matched "productivity"/"utilities",
			// includes('ai') matched "mail".
			const w = (re) => re.test(n)
			if (w(/\b(ai|llm|gpt|ml)\b/)) return 'robot-outline'
			if (w(/\b(dev|developer|development|code|coding|it)\b/)) return 'code-tags'
			if (n.includes('media') || n.includes('video') || n.includes('music') || n.includes('audio')) return 'movie-open-outline'
			if (n.includes('home') || n.includes('automation') || n.includes('iot')) return 'home-outline'
			if (n.includes('network') || n.includes('dns') || n.includes('vpn')) return 'lan-connect'
			if (n.includes('product') || n.includes('office') || n.includes('note')) return 'clipboard-text-outline'
			if (n.includes('finance') || n.includes('money') || n.includes('budget')) return 'wallet-outline'
			if (n.includes('social') || n.includes('chat') || n.includes('forum')) return 'forum-outline'
			if (n.includes('cloud') || n.includes('storage') || n.includes('sync')) return 'cloud-outline'
			if (n.includes('util') || n.includes('tool') || n.includes('other')) return 'cube-outline'
			return 'cube-outline'
		},
		getGradientBg(str) {
			let hash = 0
			const s = str || 'nivaroos'
			for (let i = 0; i < s.length; i++) {
				hash = s.charCodeAt(i) + ((hash << 5) - hash)
			}
			const idx = Math.abs(hash) % GRADIENTS.length
			return { background: GRADIENTS[idx] }
		},
		isArchCompatible(item) {
			if (!item || !item.architectures || item.architectures.length === 0) return true
			return item.architectures.includes(this.arch)
		},
		onIconError(e) {
			e.target.src = defaultAppIcon
		},
		onFormIconError(e) {
			e.target.src = defaultAppIcon
		},
		onBannerError(e, item) {
			e.target.style.display = 'none'
		},
		// The carousel only turns while it's visible (Discover tab, tab in
		// the foreground, not paused by hover/focus) and never for users who
		// asked for reduced motion. It used to tick every 7 s regardless,
		// re-rendering the store even on other tabs or when minimised.
		startHeroAutoplay() {
			if (this.heroTimer) clearInterval(this.heroTimer)
			this.heroTimer = null
			const reduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches
			if (reduced || document.hidden || this.viewMode !== 'store' || this.activeTab !== 'discover') return
			this.heroTimer = setInterval(() => {
				if (this.featuredList.length > 1 && !this.heroPaused) this.nextHero()
			}, 7000)
		},
		async fetchServerArch() {
			try {
				const res = await this.$api.sys.hardwareInfo()
				this.serverArch = (res.data && res.data.data && res.data.data.arch) || ''
			} catch (e) {
				this.serverArch = ''
			}
		},
		// Deep links: open the store on an app's details, its settings
		// (Setting in an app's menu) or the custom installer.
		handleDeepLink() {
			if (this.initialMode === 'edit' && this.initialAppName) {
				this.openEditForInstalledApp(this.initialAppName)
			} else if (this.initialMode === 'custom') {
				this.openCustomInstall()
				if (this.initialComposeYaml) {
					this.customComposeYaml = this.initialComposeYaml
					const parsed = this.yamlToFormData(this.initialComposeYaml)
					if (parsed) {
						this.formState = parsed
					} else {
						this.installerTab = 'yaml'
						this.toast(this.$t('The container\'s configuration could not be read: {reason}', { reason: this.yamlError }), 'is-warning')
					}
				}
			} else if (this.storeId !== '' && this.storeId !== 0 && this.storeId !== null) {
				this.showAppDetail(String(this.storeId))
			}
		},
		apiMessage(e, fallback) {
			return (e && e.response && e.response.data && (e.response.data.message || (typeof e.response.data.data === 'string' && e.response.data.data))) || (e && e.message) || fallback || String(e)
		},
		toast(message, type = 'is-danger', duration = 4500) {
			// Buefy toasts render HTML: app titles and server messages come
			// from store sources, so they are always escaped.
			this.$buefy.toast.open({ message: escapeHtml(message), type, position: 'is-top', duration })
		},
		nextHero() {
			this.currentHeroIndex = (this.currentHeroIndex + 1) % this.featuredList.length
		},
		prevHero() {
			this.currentHeroIndex = (this.currentHeroIndex - 1 + this.featuredList.length) % this.featuredList.length
		},
		// Loads everything; returns false (and sets loadError) when the
		// catalog couldn't be loaded, so the UI shows an error with Retry
		// instead of a blank "no apps" store.
		async initStore() {
			this.isLoading = true
			this.loadError = ''
			try {
				const results = await Promise.allSettled([
					this.fetchCategories(),
					this.fetchRecommend(),
					this.fetchStoreList(),
					this.fetchSources()
				])
				const failed = results.find((r) => r.status === 'rejected')
				if (results[2].status === 'rejected') {
					this.loadError = this.apiMessage(results[2].reason, this.$t('The app catalog could not be loaded.'))
				}
				return !failed
			} finally {
				this.isLoading = false
			}
		},
		async fetchNetworks() {
			try {
				const res = await this.$api.container.getNetworks()
				this.networks = (res && res.data && res.data.data) || []
			} catch (e) {
				this.networks = []
			}
		},
		async fetchCategories() {
			try {
				const res = await this.$openAPI.appManagement.appStore.categoryList()
				if (res && res.data && res.data.data) {
					this.cateMenu = res.data.data.filter(c => c.count > 0)
				}
			} catch (e) {
				throw e
			}
		},
		async fetchRecommend() {
			try {
				const res = await this.$openAPI.appManagement.appStore.composeAppStoreInfoList(undefined, undefined, true)
				const list = res.data?.data?.list || {}
				this.recommendList = Object.keys(list).map(id => {
					const info = list[id]
					const screenshots = Array.isArray(info.screenshot_link) ? info.screenshot_link : (info.screenshot_link ? [info.screenshot_link] : [])
					return {
						id,
						category: info.category,
						icon: info.icon,
						tagline: ice_i18n(info.tagline),
						thumbnail: info.thumbnail || screenshots[0] || '',
						screenshots,
						title: ice_i18n(info.title),
						author: info.author || info.developer,
						architectures: info.architectures || ['amd64', 'arm64']
					}
				})
			} catch (e) {
				throw e
			}
		},
		async fetchStoreList() {
			try {
				const res = await this.$openAPI.appManagement.appStore.composeAppStoreInfoList()
				const list = res.data?.data?.list || {}
				this.allAppsList = Object.keys(list).map(id => {
					const info = list[id]
					const screenshots = Array.isArray(info.screenshot_link) ? info.screenshot_link : (info.screenshot_link ? [info.screenshot_link] : [])
					return {
						id,
						category: info.category,
						icon: info.icon,
						tagline: ice_i18n(info.tagline),
						thumbnail: info.thumbnail || screenshots[0] || '',
						screenshots,
						title: ice_i18n(info.title),
						author: info.author || info.developer,
						architectures: info.architectures || ['amd64', 'arm64']
					}
				})
				this.installedList = res.data?.data?.installed || []
			} catch (e) {
				throw e
			}
		},
		async fetchSources() {
			try {
				const res = await this.$openAPI.appManagement.appStore.appStoreList()
				this.storeSourcesList = res.data?.data || []
			} catch (e) {
				throw e
			}
		},
		async refreshStore() {
			const ok = await this.initStore()
			if (ok) this.toast(this.$t('App store catalog refreshed'), 'is-success', 2000)
			else this.toast(this.loadError || this.$t('Some store data could not be refreshed.'), 'is-danger')
		},
		onNarrowSection(v) {
			if (v.startsWith('cat:')) this.selectCategoryByName(v.slice(4))
			else this.switchToStoreTab(v)
		},
		switchToStoreTab(tab) {
			this.viewMode = 'store'
			this.activeTab = tab
			if (tab !== 'category') {
				this.currentCate = { id: 0, name: 'All', count: 0 }
			}
		},
		selectCategory(cat) {
			this.viewMode = 'store'
			this.activeTab = 'category'
			this.currentCate = cat
		},
		selectCategoryByName(name) {
			const cat = this.cateMenu.find(c => c.name === name) || { id: 0, name, count: 0 }
			this.selectCategory(cat)
		},
		clearSearch() {
			this.searchQuery = ''
			this.debouncedSearch = ''
			// Typing on Discover jumped to All; clearing goes back.
			if (this.searchFromTab) {
				this.activeTab = this.searchFromTab
				this.searchFromTab = ''
			}
		},
		async showAppDetail(id) {
			// Only the latest click opens: a slow earlier response used to
			// open app A over app B.
			const seq = (this.detailSeq = (this.detailSeq || 0) + 1)
			this.detailLoadingId = id
			try {
				const res = await this.$openAPI.appManagement.appStore.composeAppStoreInfo(id)
				if (seq !== this.detailSeq) return
				if (res && res.data && res.data.data) {
					this.selectedAppDetail = { id, ...res.data.data }
				}
			} catch (e) {
				if (seq === this.detailSeq) this.toast(this.$t('Could not load the details of {app}: {reason}', { app: id, reason: this.apiMessage(e) }))
			} finally {
				if (seq === this.detailSeq) this.detailLoadingId = ''
			}
		},
		closeAppDetail() {
			this.selectedAppDetail = null
		},
		async installApp(id, item) {
			if (this.installingMap[id] !== undefined) return
			this.$set(this.installingMap, id, 5)

			try {
				const res = await this.$openAPI.appManagement.appStore.composeApp(id, {
					headers: {
						'content-type': 'application/yaml',
						accept: 'application/yaml'
					}
				})
				if (res.status === 200 && res.data) {
					let composeJSON = null
					try {
						composeJSON = YAML.parse(res.data)
					} catch (err) {
						console.warn('Failed to parse YAML', err)
					}

					if (composeJSON && composeJSON['x-casaos']?.tips?.before_install?.en_us) {
						// One window per app (a shared id made a second install
						// replace the first one's callback), and closing it
						// cancels the install instead of leaving the button
						// stuck on "Installing...".
						this.$store.commit('OPEN_WINDOW', {
							id: 'tip-editor-' + id,
							title: this.$t('Before installing {title}', { title: (item && item.title) || id }),
							component: 'TipEditorModal',
							props: {
								isDialog: true,
								composeData: composeJSON,
								onSubmit: async () => {
									await this.executeInstall(res.data, item || { title: id, id })
								},
								onCancel: () => {
									this.$delete(this.installingMap, id)
								}
							},
							width: 440,
							height: 480
						})
					} else {
						await this.executeInstall(res.data, item || { title: id, id })
					}
				} else {
					throw new Error(this.$t('Failed to fetch application compose configuration'))
				}
			} catch (e) {
				this.$delete(this.installingMap, id)
				this.toast(this.$t('Installation failed') + ': ' + this.apiMessage(e))
			}
		},
		async executeInstall(yamlData, item) {
			try {
				if (this.$messageBus) {
					this.$messageBus('appstore_install', item?.title || '')
				}
				const installRes = await this.$openAPI.appManagement.compose.installComposeApp(yamlData, false, true)
				if (installRes.status === 200) {
					this.toast(this.$t('Installation started for {title}', { title: item?.title || 'app' }), 'is-success', 3000)
				} else {
					this.$delete(this.installingMap, item.id)
					this.toast(installRes.data?.message || this.$t('Installation failed'), 'is-warning')
				}
			} catch (e) {
				this.$delete(this.installingMap, item.id)
				this.toast(this.$t('Installation failed') + ': ' + this.apiMessage(e))
			}
		},
		/* Custom Modern Container Studio Methods */
		openCustomInstall() {
			this.baseDoc = null
			this.yamlError = ''
			this.formState = createDefaultFormState()
			this.customComposeYaml = this.formDataToYaml(this.formState)
			this.selectedAppDetail = null
			this.viewMode = 'installer'
			this.installerTab = 'form'
		},
		async openEditForInstalledApp(name) {
			this.isLoading = true
			try {
				const res = await this.$openAPI.appManagement.compose.myComposeApp(name, {
					headers: {
						'content-type': 'application/yaml',
						accept: 'application/yaml'
					}
				})
				if (res.status === 200 && res.data) {
					this.customComposeYaml = res.data
					const parsedState = this.yamlToFormData(res.data)
					if (parsedState) {
						parsedState.isEditing = true
						parsedState.appName = name
						this.formState = parsedState
					}
					this.selectedAppDetail = null
					this.viewMode = 'installer'
					this.installerTab = 'form'
				}
			} catch (e) {
				this.toast(this.$t('Failed to load container configuration') + ': ' + this.apiMessage(e))
			} finally {
				this.isLoading = false
			}
		},
		async openCustomizeForApp(id, item) {
			this.isLoading = true
			try {
				const res = await this.$openAPI.appManagement.appStore.composeApp(id, {
					headers: {
						'content-type': 'application/yaml',
						accept: 'application/yaml'
					}
				})
				if (res.status === 200 && res.data) {
					this.customComposeYaml = res.data
					const parsedState = this.yamlToFormData(res.data)
					if (parsedState) {
						if (item?.title) parsedState.title = item.title
						if (item?.icon) parsedState.icon = item.icon
						parsedState.isEditing = false
						this.formState = parsedState
					}
					this.selectedAppDetail = null
					this.viewMode = 'installer'
					this.installerTab = 'form'
				}
			} catch (e) {
				this.toast(this.$t('Failed to load app compose configuration') + ': ' + this.apiMessage(e))
			} finally {
				this.isLoading = false
			}
		},
		switchInstallerTab(tab) {
			if (tab === this.installerTab) return
			if (tab === 'yaml') {
				this.customComposeYaml = this.formDataToYaml(this.formState)
			} else if (tab === 'form') {
				// Invalid YAML: stay on the YAML tab and say why, instead of
				// silently falling back to the old form (which then got
				// deployed, discarding what was typed).
				this.yamlError = ''
				const parsed = this.yamlToFormData(this.customComposeYaml)
				if (!parsed) {
					this.$buefy.toast.open({ message: escapeHtml(this.$t('The YAML has an error: {reason}', { reason: this.yamlError })), type: 'is-danger', position: 'is-top', duration: 5000 })
					return
				}
				parsed.isEditing = this.formState.isEditing
				parsed.appName = this.formState.isEditing ? this.formState.appName : parsed.appName
				this.formState = parsed
			}
			this.installerTab = tab
		},
		appendImageTag(tag) {
			const parts = (this.formState.image || '').split(':')
			const base = parts[0] || 'nginx'
			this.formState.image = `${base}:${tag}`
		},
		addPort() {
			this.formState.ports.push({ host: '', container: '', protocol: 'TCP' })
		},
		removePort(idx) {
			this.formState.ports.splice(idx, 1)
		},
		addVolume() {
			const name = this.formState.appName || this.formState.containerName || 'app'
			this.formState.volumes.push({ host: `/DATA/AppData/${name}/data`, container: '/data', mode: 'rw' })
		},
		removeVolume(idx) {
			this.formState.volumes.splice(idx, 1)
		},
		addEnv() {
			this.formState.envs.push({ key: '', value: '' })
		},
		removeEnv(idx) {
			this.formState.envs.splice(idx, 1)
		},
		addDevice() {
			this.formState.devices.push({ host: '/dev/dri', container: '/dev/dri' })
		},
		removeDevice(idx) {
			this.formState.devices.splice(idx, 1)
		},
		openImportModal() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'import-panel',
				title: this.$t('Import'),
				component: 'ImportPanel',
				props: {
					onUpdate: (yaml) => {
						this.customComposeYaml = yaml
						const parsed = this.yamlToFormData(yaml)
						if (parsed) {
							parsed.isEditing = this.formState.isEditing
							if (this.formState.isEditing) parsed.appName = this.formState.appName
							this.formState = parsed
						} else {
							this.installerTab = 'yaml'
						}
					}
				},
				width: 640,
				height: 560
			})
		},
		exportYAML() {
			try {
				const yamlOut = this.installerTab === 'form' ? this.formDataToYaml(this.formState) : this.customComposeYaml
				const blob = new Blob([yamlOut], { type: 'text/yaml;charset=utf-8' })
				const filename = `${(this.formState.title || this.formState.appName || 'compose').replace(/[^a-zA-Z0-9_-]/g, '_')}.yaml`
				FileSaver.saveAs(blob, filename)
			} catch (e) {
				console.error('Export failed', e)
			}
		},
		copyYAML() {
			try {
				copy(this.customComposeYaml)
				this.$buefy.toast.open({
					message: this.$t('Compose YAML copied to clipboard'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000
				})
			} catch (e) {
				console.error('Copy failed', e)
			}
		},
		async installFromInstaller() {
			if (this.isDeployingCustom) return
			const finalYaml = this.installerTab === 'form' ? this.formDataToYaml(this.formState) : this.customComposeYaml

			if (!finalYaml.trim()) {
				this.$buefy.toast.open({
					message: this.$t('No Docker Compose configuration to deploy'),
					type: 'is-warning',
					position: 'is-top'
				})
				return
			}

			this.isDeployingCustom = true
			try {
				let res
				if (this.formState.isEditing) {
					res = await this.$openAPI.appManagement.compose.applyComposeAppSettings(this.formState.appName, finalYaml, false, true)
				} else {
					res = await this.$openAPI.appManagement.compose.installComposeApp(finalYaml, false, true)
				}

				if (res.status === 200) {
					this.toast(this.formState.isEditing ? this.$t('Container settings saved successfully!') : this.$t('Application installation started!'), 'is-success', 4000)
					this.viewMode = 'store'
					this.activeTab = 'installed'
					this.scheduleListRefresh(4000)
				} else {
					this.toast(res.data?.message || this.$t('Operation failed'), 'is-warning', 5000)
				}
			} catch (e) {
				this.toast(this.$t('Error') + ': ' + this.apiMessage(e), 'is-danger', 6000)
			} finally {
				this.isDeployingCustom = false
			}
		},
		// Parse compose YAML into the form, remembering the whole document so
		// saving writes the form's edits back into it (see composeForm.js).
		yamlToFormData(yamlStr) {
			try {
				const doc = YAML.parse(yamlStr) || {}
				if (typeof doc !== 'object' || !doc.services) throw new Error(this.$t('No "services:" section found'))
				this.baseDoc = doc
				return docToFormState(doc)
			} catch (e) {
				this.yamlError = e.message || String(e)
				return null
			}
		},
		formDataToYaml(form) {
			return YAML.stringify(applyFormToDoc(this.baseDoc || null, form))
		},
		// Registration runs in the background on the server; the result
		// arrives as app-store:register-end / -error (see sockets below).
		async addStoreSource() {
			const url = this.newSourceUrl.trim()
			if (!url || this.addingSource) return
			if (!/^https?:\/\/[^\s/]+\/\S+\.zip(\?\S*)?$/i.test(url)) {
				this.toast(this.$t('Enter the https:// address of a store .zip file.'), 'is-warning')
				return
			}
			if (this.storeSourcesList.some((src) => (src.url || src) === url)) {
				this.toast(this.$t('This app source is already added.'), 'is-warning')
				return
			}
			this.addingSource = url
			try {
				await this.$openAPI.appManagement.appStore.registerAppStore(url)
				this.newSourceUrl = ''
				// Fallback if the result event never arrives.
				clearTimeout(this.addSourceTimer)
				this.addSourceTimer = setTimeout(() => this.onSourceRegistered(url), 60000)
			} catch (e) {
				this.addingSource = false
				this.toast(this.$t('Failed to add store source') + ': ' + this.apiMessage(e))
			}
		},
		async onSourceRegistered(url, error) {
			if (!this.addingSource) return
			clearTimeout(this.addSourceTimer)
			this.addingSource = false
			if (error) {
				this.toast(this.$t('Could not add {url}: {reason}', { url, reason: error }))
				return
			}
			await this.initStore()
			this.toast(this.$t('App source added.'), 'is-success', 3000)
		},
		removeStoreSource(src) {
			const url = src.url || src
			this.confirmWindow({
				title: this.$t('Remove app source'),
				message: escapeHtml(this.$t('Remove {url}? Its apps disappear from the store. Apps you already installed keep running.', { url })),
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				type: 'is-danger',
				onConfirm: async () => {
					this.removingSourceId = src.id
					try {
						// The API takes the source's id (it was sent the URL,
						// so removing a source never worked).
						await this.$openAPI.appManagement.appStore.unregisterAppStore(src.id)
						await this.initStore()
					} catch (e) {
						this.toast(this.$t('Could not remove the app source') + ': ' + this.apiMessage(e))
					} finally {
						this.removingSourceId = null
					}
				}
			})
		},
		sourceLabel(src) {
			const url = src.url || String(src)
			const gh = url.match(/github\.com\/([^/]+)\/([^/]+)/i)
			if (gh) return `${gh[1]}/${gh[2]}`
			try {
				return new URL(url).hostname
			} catch (e) {
				return url
			}
		},
		// Several install-end events in a row (multi-app installs) refresh
		// the 4 MB catalog once, not once per event.
		scheduleListRefresh(ms = 800) {
			clearTimeout(this.refreshListTimer)
			this.refreshListTimer = setTimeout(() => {
				if (!this.destroyed) this.fetchStoreList().catch(() => {})
			}, ms)
		}
	},
	sockets: {
		'app:install-progress'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name
			const rawProgress = props['app:progress'] || props.progress || '0'
			const num = parseInt(rawProgress, 10)
			if (name && !isNaN(num)) {
				this.$set(this.installingMap, name, num)
			}
		},
		'app:install-end'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name
			if (name && this.installingMap[name] !== undefined) {
				this.$delete(this.installingMap, name)
				this.toast(this.$t('{app} is installed.', { app: name }), 'is-success', 3000)
			}
			this.scheduleListRefresh()
		},
		// Install failures used to just reset the button with no reason.
		'app:install-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name
			if (name && this.installingMap[name] !== undefined) {
				this.$delete(this.installingMap, name)
				this.toast(this.$t('Installing {app} failed: {reason}', { app: name, reason: props.message || props['message'] || this.$t('see the app logs') }), 'is-danger', 8000)
			}
		},
		// The server sends register-error followed by register-end; an end
		// event that carries a message is a failure too.
		'app-store:register-end'(res) {
			const props = res.Properties || {}
			const url = props['app-store:url'] || props['app_store:url'] || props.url || this.addingSource
			this.onSourceRegistered(url, props.message || '')
		},
		'app-store:register-error'(res) {
			const props = res.Properties || {}
			this.onSourceRegistered(props['app-store:url'] || props['app_store:url'] || props.url || this.addingSource, props.message || this.$t('registration failed'))
		}
	}
}
</script>

<style lang="scss" scoped>
.appstore-app {
	display: flex;
	height: 100%;
	width: 100%;
	background: var(--theme-bg-window, #f8fafc); color: var(--theme-text-primary, #0f172a);
	font-family: $family-sans-serif;
	position: relative;
	overflow: hidden;
}

/* Left Sidebar */
.appstore-sidebar {
	width: 240px;
	min-width: 240px;
	background: var(--theme-bg-secondary, #ffffff); border-right: 1px solid var(--theme-card-border, #e2e8f0);
	display: flex;
	flex-direction: column;
	padding: var(--space-4) var(--space-3);
	user-select: none;
}

.sidebar-brand {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-1) var(--space-2) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, #f1f5f9);
	margin-bottom: var(--space-3);
}

.brand-icon {
	width: 32px;
	height: 32px;
	filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.05));
}

.brand-title {
	font-size: var(--font-md);
	font-weight: 700;
	line-height: 1.2;
	color: var(--theme-text-primary, #1e293b);
}

.brand-subtitle {
	font-size: var(--font-2xs);
	font-weight: 400;
	color: var(--theme-text-muted, #94a3b8);
}

.sidebar-nav {
	flex: 1;
	overflow-y: auto;
	padding-right: var(--space-1);
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	&::-webkit-scrollbar {
		width: 3px;
	}
	&::-webkit-scrollbar-thumb {
		background: var(--theme-card-border, #cbd5e1);
		border-radius: var(--radius-xs);
	}
}

.nav-item {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	width: 100%;
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: none;
	background: transparent;
	color: var(--theme-text-secondary, #475569);
	font-size: var(--font-sm);
	font-weight: 500;
	cursor: pointer;
	text-align: left;
	transition: all 0.15s ease;

	.nav-icon {
		width: 18px;
		height: 18px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		color: var(--theme-text-muted, #64748b);
		transition: color 0.15s ease;

		i.mdi {
			font-size: var(--font-md);
			line-height: 1;
			display: inline-block;
			text-rendering: geometricPrecision;
			-webkit-font-smoothing: antialiased;
			-moz-osx-font-smoothing: grayscale;
		}
	}

	&:hover {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #0f172a);

		.nav-icon {
			color: var(--theme-text-primary, #1e293b);
		}
	}

	// Same selected look as the Settings sidebar: a subtle surface and a
	// blue icon (a solid blue fill made this app look different from all
	// the others).
	&.is-active {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.06));
		color: var(--theme-text-primary, #0f172a);
		font-weight: 600;

		.nav-icon {
			color: var(--color-primary-fg);
		}

		.nav-count {
			background: var(--theme-card-subtle, #f1f5f9);
			color: var(--theme-text-secondary, #475569);
		}
	}
}

.nav-label {
	flex: 1;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.nav-count {
	font-size: var(--font-2xs);
	font-weight: 500;
	padding: 0.05rem var(--space-2);
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);
}

.nav-section-header {
	font-size: var(--font-2xs);
	font-weight: 700;
	letter-spacing: 0.08em;
	color: var(--theme-text-muted, #94a3b8);
	padding: var(--space-3) var(--space-3) var(--space-1);
	text-transform: uppercase;
}

.category-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.sidebar-footer {
	padding-top: var(--space-3);
	border-top: 1px solid var(--theme-card-border, #f1f5f9);
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.footer-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	width: 100%;
	height: 32px;
	padding: 0 var(--space-3);
	border-radius: var(--radius-sm);
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;
	line-height: 1;

	.footer-icon {
		font-size: var(--font-md);
		line-height: 1;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		vertical-align: middle;
	}

	&.custom-install-btn {
		background: rgba(59, 130, 246, 0.1);
		color: var(--color-primary-fg);
		border: 1px solid rgba(59, 130, 246, 0.35);

		.footer-icon {
			color: var(--color-primary-fg);
		}

		&:hover, &.is-active {
			background: var(--color-primary);
			color: #ffffff;
			border-color: var(--color-primary);

			.footer-icon {
				color: #ffffff;
			}
		}
	}

	&.sources-btn {
		background: var(--theme-card-subtle, #f8fafc);
		color: var(--theme-text-muted, #64748b);
		border: 1px solid var(--theme-card-border, #e2e8f0);

		.footer-icon {
			color: var(--theme-text-muted, #64748b);
		}

		&:hover {
			background: var(--theme-card-subtle, #f1f5f9);
			color: var(--theme-text-secondary, #334155);
			border-color: var(--theme-card-border, #cbd5e1);
		}
	}
}

/* Main Content Area */
.appstore-main {
	flex: 1;
	display: flex;
	flex-direction: column;
	min-width: 0;
	background: var(--theme-card-subtle, #f8fafc);
}

.main-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-6);
	background: var(--theme-titlebar-bg, #ffffff); border-bottom: 1px solid var(--theme-card-border, #e2e8f0);
	gap: var(--space-4);
}

.search-wrapper {
	position: relative;
	flex: 1;
	max-width: 440px;
	display: flex;
	align-items: center;
}

.search-icon {
	position: absolute;
	left: 0.875rem;
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-md);
	pointer-events: none;
}

.search-input {
	-webkit-appearance: none;
	appearance: none;
	width: 100%;
	padding: var(--space-2) var(--space-8) var(--space-2) 2.35rem;
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-card-border, #cbd5e1);
	background: var(--theme-input-bg, #f8fafc);
	font-size: var(--font-sm);
	color: var(--theme-text-primary, #0f172a); border-color: var(--theme-input-border, #cbd5e1);
	outline: none;
	transition: all 0.15s ease;

	&:focus {
		border-color: var(--color-primary-fg);
		background: var(--theme-card-bg, #ffffff);
	}

	&::-webkit-search-cancel-button {
		display: none;
	}
}

.clear-search-btn {
	position: absolute;
	right: 0.75rem;
	border: none;
	background: transparent;
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-base);
	cursor: pointer;

	&:hover {
		color: var(--theme-text-secondary, #475569);
	}
}

.header-actions {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.filter-btn {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-card-border, #cbd5e1);
	background: var(--theme-card-bg, #ffffff);
	color: var(--theme-text-primary, #334155);
	font-size: var(--font-sm);
	font-weight: 500;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-base);
	}

	&:hover {
		background: var(--theme-card-subtle, #f1f5f9);
	}
}

.icon-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 32px;
	height: 32px;
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-card-border, #cbd5e1);
	background: var(--theme-card-bg, #ffffff);
	color: var(--theme-text-secondary, #475569);
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-md);
	}

	&:hover {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #0f172a);
	}

	&.is-spinning i {
		display: inline-block;
		animation: spin 0.8s linear infinite;
	}
}

@keyframes spin {
	100% {
		transform: rotate(360deg);
	}
}

.main-body {
	flex: 1;
	overflow-y: auto;
	padding: var(--space-5) var(--space-6) var(--space-8);
}

/* Discover Section & Hero Banner */
.hero-carousel-wrapper {
	margin-bottom: var(--space-8);
}

.hero-carousel {
	position: relative;
	height: 240px;
	border-radius: var(--radius-modal);
	overflow: hidden;
	// Always dark, in both themes: all of the hero's text is light.
	background: #0f172a;
}

.hero-slide {
	position: absolute;
	inset: 0;
	opacity: 0;
	transition: opacity 0.35s ease;
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-8) var(--space-8);
	pointer-events: none;

	&.is-active {
		opacity: 1;
		pointer-events: auto;
	}
}

.hero-ambient-glow {
	position: absolute;
	inset: 0;
	background: radial-gradient(circle at 80% 50%, rgba(37, 99, 235, 0.22) 0%, rgba(15, 23, 42, 0.96) 70%);
	z-index: 1;
}

.hero-content {
	position: relative;
	z-index: 2;
	max-width: 460px;
	color: #ffffff;
}

.hero-badge {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	background: rgba(234, 179, 8, 0.18);
	border: 1px solid rgba(234, 179, 8, 0.35);
	color: #fbbf24;
	font-size: var(--font-2xs);
	font-weight: 700;
	letter-spacing: 0.05em;
	margin-bottom: var(--space-3);
	i.mdi {
		font-size: var(--font-xs);
	}
}

.hero-app-info {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	margin-bottom: var(--space-2);
}

.hero-app-icon {
	width: 44px;
	height: 44px;
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #ffffff);
	padding: var(--space-1);
	flex-shrink: 0;
}

.hero-text-col {
	display: flex;
	flex-direction: column;
}

.hero-app-title {
	font-size: var(--font-xl);
	font-weight: 700;
	color: #ffffff;
	line-height: 1.2;
}

.hero-app-meta {
	font-size: var(--font-xs);
	color: rgba(255, 255, 255, 0.72);
	font-weight: 400;
}

.hero-app-tagline {
	font-size: var(--font-sm);
	color: rgba(255, 255, 255, 0.82);
	text-shadow: none !important;
	display: -webkit-box;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
	overflow: hidden;
	line-height: 1.4;
	margin-bottom: var(--space-4);
}

.hero-actions {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.hero-action-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-4);
	border-radius: var(--radius-pill);
	font-size: var(--font-sm);
	font-weight: 600;
	border: none;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-base);
	}

	&.is-install {
		background: var(--color-primary);
		color: #ffffff;

		&:hover {
			background: var(--color-primary-hover);
		}

		&.is-loading {
			background: #3b82f6;
		}
	}

	&.is-customize {
		background: rgba(255, 255, 255, 0.15);
		color: #ffffff;
		border: 1px solid rgba(255, 255, 255, 0.25);
		backdrop-filter: blur(8px);

		&:hover {
			background: rgba(255, 255, 255, 0.25);
		}
	}

	&.is-open {
		background: rgba(255, 255, 255, 0.2);
		color: #ffffff;
		backdrop-filter: blur(8px);

		&:hover {
			background: rgba(255, 255, 255, 0.3);
		}
	}
}

.hero-details-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-pill);
	background: transparent;
	color: rgba(255, 255, 255, 0.82);
	font-size: var(--font-sm);
	font-weight: 500;
	border: none;
	cursor: pointer;

	i.mdi {
		font-size: var(--font-sm);
	}

	&:hover {
		color: #ffffff;
	}
}

.hero-preview-box {
	padding: 0;
	border: none;
	font: inherit;
	position: relative;
	z-index: 2;
	width: 250px;
	height: 145px;
	border-radius: var(--radius-card);
	overflow: hidden;
	border: 1px solid rgba(255, 255, 255, 0.12);
	cursor: pointer;
	transition: transform 0.2s ease;
	/* Fixed dark placeholder bg (not theme-reactive): sits inside the
	   always-dark hero banner overlay before the screenshot image loads. */
	background: #0f172a;

	&:hover {
	}
}

.hero-preview-img {
	width: 100%;
	height: 100%;
	object-fit: cover;
	object-position: top;
}

.carousel-arrow {
	position: absolute;
	top: 50%;
	transform: translateY(-50%);
	z-index: 5;
	width: 28px;
	height: 28px;
	border-radius: 50%;
	background: rgba(15, 23, 42, 0.6);
	backdrop-filter: blur(4px);
	border: 1px solid rgba(255, 255, 255, 0.15);
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-md);
	}

	&:hover {
		background: rgba(15, 23, 42, 0.9);
	}

	&.is-prev {
		left: 0.75rem;
	}
	&.is-next {
		right: 0.75rem;
	}
}

.carousel-dots {
	position: absolute;
	bottom: 0.75rem;
	right: 1.25rem;
	z-index: 5;
	display: flex;
	gap: var(--space-1);
}

.carousel-dot {
	width: 6px;
	height: 6px;
	border-radius: var(--radius-pill);
	background: rgba(255, 255, 255, 0.35);
	border: none;
	cursor: pointer;
	transition: all 0.2s ease;

	&.is-active {
		width: 16px;
		background: #ffffff;
	}
}

/* Sections */
.section-block {
	margin-bottom: var(--space-8);
}

.section-header {
	display: flex;
	align-items: flex-end;
	justify-content: space-between;
	margin-bottom: var(--space-4);
}

.section-title {
	font-size: var(--font-lg);
	font-weight: 700;
	color: var(--theme-text-primary, #0f172a);
	margin-bottom: var(--space-1);
}

.section-subtitle {
	font-size: var(--font-sm);
	font-weight: 400;
	color: var(--theme-text-muted, #64748b);
}

.see-all-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	background: transparent;
	border: none;
	color: var(--color-primary-fg);
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;

	i.mdi {
		font-size: var(--font-sm);
	}

	&:hover {
		color: var(--color-primary-fg);
	}
}

.catalog-header {
	margin-bottom: var(--space-4);
}

.catalog-title {
	font-size: var(--font-xl);
	font-weight: 700;
	color: var(--theme-text-primary, #0f172a);
	margin-bottom: var(--space-1);
}

.catalog-subtitle {
	font-size: var(--font-sm);
	font-weight: 400;
	color: var(--theme-text-muted, #64748b);
}

/* App Cards Grid */
.app-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
	gap: var(--space-5);
}

.app-card {
	background: var(--theme-card-bg, #ffffff); border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-card);
	overflow: hidden;
	display: flex;
	flex-direction: column;
	cursor: pointer;
	transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
	text-shadow: none !important;

	* {
		text-shadow: none !important;
	}

	&:hover {
		border-color: var(--theme-card-border, #cbd5e1);

		.card-banner-img {
		}
	}
}

.card-banner {
	position: relative;
	width: 100%;
	height: 145px;
	overflow: hidden;
	/* Fixed dark placeholder bg (not theme-reactive): the placeholder-icon
	   below is a translucent white icon that assumes an always-dark backdrop
	   before the real banner image loads, in both light and dark mode. */
	background: #0f172a;
}

.card-banner-img {
	width: 100%;
	height: 100%;
	object-fit: cover;
	object-position: top;
	transition: transform 0.3s ease;
}

.card-banner-placeholder {
	width: 100%;
	height: 100%;
	display: flex;
	align-items: center;
	justify-content: center;
}

.placeholder-icon {
	color: rgba(255, 255, 255, 0.3);
	font-size: var(--font-2xl);
}

.app-card-body {
	padding: var(--space-4);
	display: flex;
	flex-direction: column;
	flex: 1;
}

.app-card-top {
	display: flex;
	gap: var(--space-3);
	margin-bottom: var(--space-2);
}

.app-icon {
	width: 40px;
	height: 40px;
	border-radius: var(--radius-control);
	object-fit: cover;
	flex-shrink: 0;
	background: var(--theme-card-subtle, #f8fafc);
	padding: var(--space-1);
	border: 1px solid var(--theme-card-border, #f1f5f9);
}

.app-info {
	flex: 1;
	min-width: 0;
}

.app-title {
	font-size: var(--font-base);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	line-height: 1.3;
}

.app-author {
	font-size: var(--font-xs);
	font-weight: 400;
	color: var(--theme-text-muted, #94a3b8);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	display: block;
}

.app-tagline {
	font-size: var(--font-sm);
	font-weight: 400;
	color: var(--theme-text-secondary, #64748b);
	line-height: 1.4;
	text-shadow: none !important;
	display: -webkit-box;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
	overflow: hidden;
	margin-bottom: var(--space-3);
	flex: 1;
}

.app-card-bottom {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding-top: var(--space-2);
	border-top: 1px solid var(--theme-card-border, #f1f5f9);
	gap: var(--space-2);
}

.app-meta-group {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	min-width: 0;
	flex: 1;
	overflow: hidden;
}

.app-cat-pill {
	font-size: var(--font-2xs);
	font-weight: 500;
	color: var(--theme-pill-color, #475569); background: var(--theme-pill-bg, #f1f5f9);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	white-space: nowrap;
}

.app-arch-text {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
	font-weight: 400;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.card-btn {
	padding: var(--space-1) var(--space-4);
	border-radius: var(--radius-pill);
	font-size: var(--font-sm);
	font-weight: 600;
	border: none;
	cursor: pointer;
	transition: all 0.15s ease;
	white-space: nowrap;

	&.is-install {
		background: var(--color-primary);
		color: #ffffff;

		&:hover {
			background: var(--color-primary-hover);
		}

		&:disabled {
			background: #93c5fd;
			color: #ffffff;
			cursor: not-allowed;
		}
	}

	&.is-open {
		background: rgba(59, 130, 246, 0.1);
		color: var(--color-primary-fg);
		border: 1px solid rgba(59, 130, 246, 0.35);

		&:hover {
			background: rgba(59, 130, 246, 0.12);
		}
	}
}

.card-btn-split {
	display: inline-flex;
	align-items: center;
	border-radius: var(--radius-pill);
	overflow: hidden;
	background: var(--color-primary);

	.card-btn.is-install {
		border-radius: 0;
		padding: var(--space-1) var(--space-3);
	}

	.card-btn-cog {
		background: var(--color-primary-hover);
		color: #ffffff;
		border: none;
		padding: var(--space-1) var(--space-2);
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: background 0.15s ease;

		i.mdi {
			font-size: var(--font-sm);
		}

		&:hover {
			background: var(--color-primary-hover);
		}

		&:disabled {
			background: var(--theme-card-border, #cbd5e1);
			cursor: not-allowed;
		}
	}
}

/* Custom Modern Container Studio & Installer View */
.appstore-installer {
	flex: 1;
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--theme-card-subtle, #f1f5f9);
	overflow: hidden;
}

.installer-header {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-6);
	border-bottom: 1px solid var(--theme-card-border, #e2e8f0);
	background: var(--theme-titlebar-bg, #ffffff);
	gap: var(--space-4);
}

.installer-header-left {
	display: flex;
	align-items: center;
	gap: var(--space-4);
}

.back-to-store-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	background: var(--theme-card-subtle, #f8fafc);
	color: var(--theme-text-secondary, #475569);
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-md);
	}

	&:hover {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #0f172a);
	}
}

.installer-app-badge {
	display: flex;
	align-items: center;
	gap: var(--space-3);
}

.installer-app-icon {
	width: 32px;
	height: 32px;
	border-radius: var(--radius-sm);
	background: var(--theme-card-subtle, #f8fafc);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	padding: var(--space-1);
	object-fit: cover;
}

.installer-app-info {
	display: flex;
	flex-direction: column;
}

.installer-app-title {
	font-size: var(--font-md);
	font-weight: 700;
	color: var(--theme-text-primary, #0f172a);
	line-height: 1.2;
}

.installer-app-sub {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #64748b);
	font-family: monospace;
}

.view-switch-pills {
	display: inline-flex;
	align-items: center;
	background: var(--theme-card-subtle, #f1f5f9);
	padding: var(--space-1);
	border-radius: var(--radius-control);
	border: 1px solid var(--theme-card-border, #e2e8f0);
}

.view-pill-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-1) var(--space-4);
	border-radius: var(--radius-sm);
	border: none;
	background: transparent;
	color: var(--theme-text-muted, #64748b);
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-md);
	}

	&.is-active {
		background: var(--theme-card-bg, #ffffff);
		color: var(--color-primary-fg);
	}
}

.installer-header-right {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.tool-action-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-card-border, #cbd5e1);
	background: var(--theme-card-bg, #ffffff);
	color: var(--theme-text-secondary, #475569);
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-md);
	}

	&:hover {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #0f172a);
	}
}

.installer-body {
	flex: 1;
	overflow-y: auto;
	padding: var(--space-5) var(--space-8) var(--space-8);
}

/* Card-Based Visual Editor */
.installer-form-layout {
	max-width: 960px;
	margin: 0 auto;
	display: flex;
	flex-direction: column;
	gap: var(--space-5);
}

.installer-card {
	background: var(--theme-card-bg, #ffffff);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-card);
	padding: var(--space-5) var(--space-6);
}

.card-title-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	margin-bottom: var(--space-2);
	&.is-clickable {
		cursor: pointer;
		margin-bottom: 0;
	}
}

.card-title-left {
	flex: 1;
}

.card-icon-pill {
	width: 32px;
	height: 32px;
	border-radius: var(--radius-control);
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: var(--font-lg);
	flex-shrink: 0;

	&.is-blue { background: rgba(37, 99, 235, 0.12); color: var(--color-primary-fg); }
	&.is-emerald { background: rgba(5, 150, 105, 0.12); color: var(--color-success-fg); }
	&.is-indigo { background: rgba(79, 70, 229, 0.12); color: var(--color-accent-fg); }
	&.is-amber { background: var(--theme-warning-soft, rgba(217, 119, 6, 0.12)); color: var(--color-warning-fg); }
	&.is-purple { background: rgba(147, 51, 234, 0.12); color: var(--color-accent-fg); }
	&.is-slate { background: var(--theme-card-subtle, rgba(71, 85, 105, 0.12)); color: var(--theme-text-secondary, #475569); }
}

.card-heading {
	font-size: var(--font-md);
	font-weight: 700;
	color: var(--theme-text-primary, #0f172a);
	line-height: 1.2;
}

.card-caption {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}

.card-toggle-wrap {
	margin-left: auto;
}

.accordion-toggle-btn {
	background: transparent;
	border: none;
	color: var(--theme-text-muted, #64748b);
	font-size: var(--font-xl);
	cursor: pointer;
}

/* Forms & Inputs */
// auto-fit instead of fixed 2/3 columns: they overflowed a phone-width
// window and were cramped at tablet width.
.form-grid-2 {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(min(15rem, 100%), 1fr));
	gap: var(--space-4);
}

.form-grid-3 {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(min(10rem, 100%), 1fr));
	gap: var(--space-4);
}

.form-group {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.form-label {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-secondary, #334155);

	.req {
		color: var(--color-danger-fg);
	}
}

.form-input, .form-select {
	width: 100%;
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-input-border, #94a3b8);
	background: var(--theme-input-bg, #ffffff);
	font-size: var(--font-sm);
	color: var(--theme-text-primary, #0f172a);
	outline: none;
	transition: all 0.15s ease;

	&:focus {
		border-color: var(--color-primary-fg);
		background: var(--theme-card-bg, #ffffff);
		box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.1);
	}

	&.is-sm {
		padding: var(--space-2) var(--space-2);
		font-size: var(--font-sm);
	}

	&.is-mono {
		font-family: monospace;
	}
}

.quick-tags {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	margin-top: var(--space-1);
}

.quick-tag-label {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
}

.quick-tag-btn {
	background: var(--theme-card-subtle, #f1f5f9);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	color: var(--theme-text-secondary, #475569);
	border-radius: var(--radius-xs);
	padding: var(--space-1) var(--space-1);
	font-size: var(--font-2xs);
	font-family: monospace;
	cursor: pointer;

	&:hover {
		background: var(--theme-card-border, #e2e8f0);
		color: var(--theme-text-primary, #0f172a);
	}
}

.icon-input-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
}

.icon-preview-thumb {
	width: 38px;
	height: 38px;
	border-radius: var(--radius-control);
	background: var(--theme-card-subtle, #f8fafc);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	padding: var(--space-1);
	object-fit: cover;
	flex-shrink: 0;
}

/* Dynamic Rows */
.dynamic-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.dynamic-row {
	display: flex;
	flex-wrap: wrap;
	align-items: flex-end;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-control);
	background: var(--theme-card-subtle, #f8fafc);
	border: 1px solid var(--theme-card-border, #e2e8f0);
}

.dynamic-field {
	flex: 1 1 8rem;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	&.is-wide {
		flex: 2;
	}

	&.is-protocol {
		flex: 0 1 90px;
	}

	&.is-mode {
		flex: 0 1 160px;
	}
}

.field-mini-label {
	font-size: var(--font-2xs);
	font-weight: 600;
	text-transform: uppercase;
	color: var(--theme-text-muted, #94a3b8);
}

.row-arrow {
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-base);
	margin-bottom: var(--space-2);
}

.delete-row-btn {
	background: transparent;
	border: none;
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-lg);
	cursor: pointer;
	padding: var(--space-1);
	margin-bottom: var(--space-1);
	border-radius: var(--radius-xs);
	display: flex;
	align-items: center;
	justify-content: center;

	&:hover {
		background: rgba(239, 68, 68, 0.1);
		color: var(--color-danger-fg);
	}
}

.add-row-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-1);
	width: 100%;
	padding: var(--space-2);
	border-radius: var(--radius-control);
	border: 1px dashed var(--theme-card-border, #cbd5e1);
	background: var(--theme-card-bg, #ffffff);
	color: var(--color-primary-fg);
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: var(--font-md);
	}

	&:hover {
		background: rgba(59, 130, 246, 0.1);
		border-color: #93c5fd;
	}
}

/* YAML Studio */
.yaml-studio-wrapper {
	max-width: 960px;
	margin: 0 auto;
	height: 100%;
	display: flex;
	flex-direction: column;
	border-radius: var(--radius-card);
	overflow: hidden;
	border: 1px solid #1e293b;
}

.yaml-studio-bar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-2) var(--space-4);
	background: #090d16;
	border-bottom: 1px solid #1e293b;
}

.yaml-stat {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-xs);
	font-family: monospace;
	color: var(--theme-text-muted, #94a3b8);
}

.copy-yaml-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-xs);
	border: 1px solid #334155;
	background: #1e293b;
	color: var(--theme-text-muted, #cbd5e1);
	font-size: var(--font-2xs);
	cursor: pointer;

	&:hover {
		background: #334155;
		color: #ffffff;
	}
}

.yaml-studio-textarea {
	flex: 1;
	min-height: 480px;
	width: 100%;
	padding: var(--space-5);
	/* Fixed dark code-editor bg (not theme-reactive): must match the
	   always-dark .yaml-studio-bar/border chrome above and the light
	   cyan text color, in both light and dark mode. */
	background: #0f172a;
	color: #38bdf8;
	font-family: 'JetBrains Mono', 'Fira Code', Consolas, Monaco, monospace;
	font-size: var(--font-sm);
	line-height: 1.6;
	border: none;
	outline: none;
	resize: none;
}

/* Installer Footer */
.installer-footer {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-4) var(--space-8);
	border-top: 1px solid var(--theme-card-border, #e2e8f0);
	background: var(--theme-card-bg, #ffffff);
	gap: var(--space-4);
}

.installer-footer-info {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);

	i.mdi {
		color: var(--color-primary-fg);
		font-size: var(--font-md);
	}
}

.installer-footer-btns {
	display: flex;
	align-items: center;
	gap: var(--space-3);
}

.installer-cancel-btn {
	padding: var(--space-2) var(--space-5);
	border-radius: var(--radius-sm);
	background: transparent;
	border: 1px solid var(--theme-card-border, #cbd5e1);
	color: var(--theme-text-secondary, #475569);
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;

	&:hover {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #0f172a);
	}
}

.installer-deploy-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-6);
	border-radius: var(--radius-sm);
	background: var(--color-primary);
	border: none;
	color: #ffffff;
	font-size: var(--font-sm);
	font-weight: 600;
	cursor: pointer;
	transition: background 0.15s ease;

	i.mdi {
		font-size: var(--font-md);
	}

	&:hover {
		background: var(--color-primary-hover);
	}

	&:disabled {
		background: var(--theme-card-border, #cbd5e1);
		cursor: not-allowed;
	}
}

/* Skeletons */
.app-card.is-skeleton {
	pointer-events: none;
}

.skeleton-banner {
	width: 100%;
	height: 145px;
	background: var(--theme-card-border, #e2e8f0);
	animation: pulse 1.5s infinite;
}

.skeleton-icon {
	width: 40px;
	height: 40px;
	border-radius: var(--radius-control);
	background: var(--theme-card-border, #e2e8f0);
	animation: pulse 1.5s infinite;
}

.skeleton-info {
	flex: 1;
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.skeleton-line {
	height: 11px;
	background: var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-xs);
	animation: pulse 1.5s infinite;

	&.is-title {
		width: 70%;
	}
	&.is-subtitle {
		width: 90%;
	}
	&.is-tag {
		width: 45px;
		margin-top: var(--space-2);
	}
}

@keyframes pulse {
	0%, 100% { opacity: 1; }
	50% { opacity: 0.5; }
}

/* Empty State */
.empty-state {
	text-align: center;
	padding: var(--space-14) var(--space-8);
	color: var(--theme-text-muted, #64748b);
}

.empty-icon {
	color: var(--theme-text-muted, #cbd5e1);
	font-size: 40px;
	margin-bottom: var(--space-3);
}

.empty-title {
	font-size: var(--font-lg);
	font-weight: 600;
	color: var(--theme-text-secondary, #334155);
	margin-bottom: var(--space-2);
}

.empty-desc {
	font-size: var(--font-sm);
	margin-bottom: var(--space-5);
}

.empty-action-btn {
	padding: var(--space-2) var(--space-5);
	border-radius: var(--radius-sm);
	background: var(--color-primary);
	color: #ffffff;
	border: none;
	font-weight: 600;
	cursor: pointer;
}

/* App detail window (a SettingsOverlay window) */
.detail-window {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.detail-hero {
	display: flex;
	gap: var(--space-5);
	margin-bottom: var(--space-8);
	padding-bottom: var(--space-5);
	border-bottom: 1px solid var(--theme-card-border, #f1f5f9);
}

.detail-icon {
	width: 64px;
	height: 64px;
	border-radius: var(--radius-modal);
	flex-shrink: 0;
	border: 1px solid var(--theme-card-border, #f1f5f9);
}

.detail-hero-info {
	flex: 1;
}

.detail-title {
	font-size: var(--font-xl);
	font-weight: 700;
	color: var(--theme-text-primary, #0f172a);
	margin-bottom: var(--space-1);
}

.detail-tagline {
	font-size: var(--font-sm);
	color: var(--theme-text-muted, #64748b);
	margin-bottom: var(--space-3);
	line-height: 1.4;
}

.detail-meta-row {
	display: flex;
	gap: var(--space-2);
	margin-bottom: var(--space-4);
	flex-wrap: wrap;
}

.detail-pill {
	font-size: var(--font-xs);
	font-weight: 500;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	background: rgba(59, 130, 246, 0.1);
	color: var(--color-primary-fg);

	&.is-subtle {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-muted, #64748b);
	}

	&.is-danger {
		background: rgba(239, 68, 68, 0.08);
		color: var(--color-danger-fg);
	}
}

.detail-actions {
	display: flex;
	gap: var(--space-3);
}

.detail-action-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-5);
	border-radius: var(--radius-pill);
	font-size: var(--font-sm);
	font-weight: 600;
	border: none;
	cursor: pointer;

	i.mdi {
		font-size: var(--font-base);
	}

	&.is-install {
		background: var(--color-primary);
		color: #ffffff;

		&:hover {
			background: var(--color-primary-hover);
		}

		&.is-loading {
			background: #3b82f6;
		}
	}

	&.is-customize {
		background: rgba(59, 130, 246, 0.1);
		color: var(--color-primary-fg);
		border: 1px solid rgba(59, 130, 246, 0.35);

		&:hover {
			background: rgba(59, 130, 246, 0.12);
		}
	}

	&.is-open {
		background: rgba(59, 130, 246, 0.1);
		color: var(--color-primary-fg);
		border: 1px solid rgba(59, 130, 246, 0.35);

		&:hover {
			background: rgba(59, 130, 246, 0.12);
		}
	}
}

.detail-section {
	margin-bottom: var(--space-8);
}

.detail-section-title {
	font-size: var(--font-md);
	font-weight: 700;
	color: var(--theme-text-primary, #0f172a);
	margin-bottom: var(--space-3);
}

.screenshots-gallery {
	display: flex;
	gap: var(--space-4);
	overflow-x: auto;
	padding-bottom: var(--space-2);
	&::-webkit-scrollbar {
		height: 5px;
	}
	&::-webkit-scrollbar-thumb {
		background: var(--theme-card-border, #cbd5e1);
		border-radius: var(--radius-xs);
	}
}

.screenshot-item {
	padding: 0;
	border: none;
	background: none;
	cursor: zoom-in;
	position: relative;
	flex-shrink: 0;
	width: 250px;
	height: 145px;
	border-radius: var(--radius-card);
	overflow: hidden;
	border: 1px solid var(--theme-card-border, #e2e8f0);
	cursor: pointer;
	transition: transform 0.15s ease;

	&:hover {

		.screenshot-hover-overlay {
			opacity: 1;
		}
	}

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		object-position: top;
	}
}

.screenshot-hover-overlay {
	position: absolute;
	inset: 0;
	background: rgba(15, 23, 42, 0.4);
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: var(--font-xl);
	opacity: 0;
	transition: opacity 0.2s ease;
}

.detail-description {
	background: var(--theme-card-subtle, #f8fafc);
	border-radius: var(--radius-card);
	padding: var(--space-4);
	border: 1px solid var(--theme-card-border, #e2e8f0);
}

.description-text {
	font-size: var(--font-sm);
	line-height: 1.55;
	color: var(--theme-text-secondary, #334155);
	white-space: pre-line;
	text-shadow: none !important;
}

.specs-grid {
	display: grid;
	grid-template-columns: repeat(2, 1fr);
	gap: var(--space-3);
}

.spec-card {
	background: var(--theme-card-subtle, #f8fafc); border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-control);
	padding: var(--space-3) var(--space-3);
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.spec-label {
	font-size: var(--font-2xs);
	font-weight: 600;
	text-transform: uppercase;
	color: var(--theme-text-muted, #94a3b8);
}

.spec-value {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b);
}

/* Screenshot window */
.lightbox-img {
	display: block;
	max-width: 100%;
	max-height: 75vh;
	margin: 0 auto;
	border-radius: var(--radius-control);
}

/* App Sources window */
.sources-intro,
.sources-hint {
	margin: 0 0 var(--space-3);
	font-size: var(--font-sm);
	color: var(--theme-text-secondary, #475569);
}

.sr-only {
	position: absolute;
	width: 1px;
	height: 1px;
	overflow: hidden;
	clip: rect(0 0 0 0);
	white-space: nowrap;
}

.add-source-box {
	display: flex;
	gap: var(--space-2);
	margin-bottom: var(--space-4);
}

.source-input {
	flex: 1;
	min-width: 0;
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-input-border, #94a3b8);
	background: var(--theme-input-bg, #ffffff);
	color: var(--theme-text-primary, #0f172a);
	font-size: var(--font-sm);

	&:focus {
		border-color: var(--color-primary-fg);
		outline: none;
	}
}

.add-source-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	background: var(--color-primary);
	color: #ffffff;
	border: none;
	font-weight: 600;
	font-size: var(--font-sm);
	cursor: pointer;
	white-space: nowrap;

	&:disabled {
		background: var(--theme-card-subtle, #e2e8f0);
		color: var(--theme-text-secondary, #475569);
		cursor: not-allowed;
	}
}

.sources-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.source-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-2) var(--space-3);
	background: var(--theme-card-subtle, #f8fafc);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-sm);
}

.source-row-icon {
	color: var(--theme-text-muted, #5b6779);
	font-size: var(--font-lg);
}

.source-info {
	flex: 1;
	min-width: 0;
	display: flex;
	flex-direction: column;
}

.source-name {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
}

.source-url {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #5b6779);
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.delete-source-btn {
	border: none;
	background: transparent;
	color: var(--color-danger-fg);
	cursor: pointer;
	padding: var(--space-1);
	font-size: var(--font-md);
	border-radius: var(--radius-sm);

	&:hover:not(:disabled) {
		background: var(--color-danger-soft, rgba(239, 68, 68, 0.1));
	}
}

/* Responsive Adaptations */
.appstore-app.is-compact {
	.appstore-sidebar {
		width: 56px;
		min-width: 56px;
		padding: var(--space-3) var(--space-1);
		.brand-info, .nav-label, .nav-count, .nav-section-header, .footer-btn span {
			display: none;
		}

		.nav-item, .footer-btn {
			justify-content: center;
			padding: var(--space-2);
		}
	}

	.main-header {
		padding: var(--space-3) var(--space-4);
	}

	.main-body {
		padding: var(--space-4) var(--space-4) var(--space-8);
	}

	.hero-preview-box {
		display: none;
	}
}

.appstore-app.is-narrow {
	.appstore-sidebar {
		display: none;
	}
}

/* Transitions */
.fade-enter-active, .fade-leave-active {
	transition: opacity 0.2s ease;
}

.fade-enter, .fade-leave-to {
	opacity: 0;
}
.narrow-nav {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, #e2e8f0);
}

.narrow-select {
	flex: 1;
	min-width: 0;
	height: 2.25rem;
	padding: 0 var(--space-2);
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-input-border, #94a3b8);
	background: var(--theme-input-bg, #ffffff);
	color: var(--theme-text-primary, #0f172a);
	font-size: var(--font-sm);
}

.store-error {
	display: flex;
	flex-direction: column;
	align-items: center;
	text-align: center;
	gap: var(--space-2);
	padding: var(--space-8) var(--space-4);
}

.store-error-icon {
	font-size: 3rem;
	color: var(--color-danger-fg);
}

// Keyboard focus ring everywhere in the store (it had none, and several
// inputs set outline:none), matching Settings' :focus-visible rule.
.appstore-app :focus-visible {
	outline: 2px solid var(--color-primary-fg);
	outline-offset: 2px;
}

// Fields show focus with their own border colour + a tight ring.
.appstore-app input:focus-visible,
.appstore-app select:focus-visible,
.appstore-app textarea:focus-visible {
	outline-offset: -1px;
}
.installer-errors {
	display: flex;
	gap: var(--space-2);
	margin: 0 var(--space-6);
	padding: var(--space-2) var(--space-3);
	border-left: 3px solid var(--color-danger-fg);
	border-radius: var(--radius-sm);
	background: var(--color-danger-soft, rgba(239, 68, 68, 0.1));
	color: var(--theme-text-primary, #0f172a);
	font-size: var(--font-sm);

	.mdi {
		color: var(--color-danger-fg);
	}

	ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
}
// Narrow windows: less padding around the installer.
.appstore-app.is-compact {
	.installer-body {
		padding: var(--space-4);
	}

	.installer-footer {
		padding: var(--space-3) var(--space-4);
		flex-wrap: wrap;
	}

	.installer-header {
		padding: var(--space-2) var(--space-4);
	}

	.installer-errors {
		margin: 0 var(--space-4);
	}

	.hero-actions,
	.detail-actions,
	.main-header,
	.header-actions {
		flex-wrap: wrap;
	}
}
.hero-actions,
.detail-actions {
	flex-wrap: wrap;
}
</style>
