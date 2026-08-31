<template>
	<div class="appstore-app" :class="{ 'is-compact': isCompact, 'is-narrow': isNarrow }">
		<!-- Left Navigation Sidebar -->
		<aside class="appstore-sidebar">
			<div class="sidebar-brand">
				<img :src="appStoreIcon" class="brand-icon" alt="App Store" />
				<div class="brand-info">
					<h2 class="brand-title">{{ $t('App Store') }}</h2>
					<span class="brand-subtitle">{{ totalAppCount }} {{ $t('Applications') }}</span>
				</div>
			</div>

			<div class="sidebar-nav">
				<button
					class="nav-item"
					:class="{ 'is-active': viewMode === 'store' && activeTab === 'discover' }"
					@click="switchToStoreTab('discover')"
				>
					<span class="nav-icon"><i class="mdi mdi-compass-outline"></i></span>
					<span class="nav-label">{{ $t('Discover') }}</span>
				</button>

				<button
					class="nav-item"
					:class="{ 'is-active': viewMode === 'store' && activeTab === 'all' }"
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
						v-for="cat in cateMenu"
						:key="cat.id"
						class="nav-item category-item"
						:class="{ 'is-active': viewMode === 'store' && activeTab === 'category' && currentCate.name === cat.name }"
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
					class="nav-item"
					:class="{ 'is-active': viewMode === 'store' && activeTab === 'installed' }"
					@click="switchToStoreTab('installed')"
				>
					<span class="nav-icon"><i class="mdi mdi-check-circle-outline"></i></span>
					<span class="nav-label">{{ $t('Installed') }}</span>
					<span class="nav-count">{{ installedList.length }}</span>
				</button>
			</div>

			<div class="sidebar-footer">
				<button class="footer-btn custom-install-btn" :class="{ 'is-active': viewMode === 'installer' }" @click="openCustomInstall">
					<i class="mdi mdi-plus footer-icon"></i>
					<span>{{ $t('Custom Install') }}</span>
				</button>

				<button class="footer-btn sources-btn" @click="showSourcesModal = true">
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
						type="text"
						class="search-input"
						:placeholder="$t('Search 400+ self-hosted apps, tools, and servers...')"
						@input="onSearchInput"
					/>
					<button v-if="searchQuery" class="clear-search-btn" @click="clearSearch">
						<i class="mdi mdi-close-circle"></i>
					</button>
				</div>

				<div class="header-actions">
					<!-- Store Sources Dropdown -->
					<b-dropdown v-model="currentAuthor" aria-role="list" class="source-dropdown">
						<template #trigger="{ active }">
							<button class="filter-btn">
								<i class="mdi mdi-storefront-outline"></i>
								<span>{{ currentAuthor.name }}</span>
								<i :class="'mdi ' + (active ? 'mdi-chevron-up' : 'mdi-chevron-down')"></i>
							</button>
						</template>
						<b-dropdown-item
							v-for="item in authorMenu"
							:key="item.name"
							:value="item"
							:class="{ 'is-active': currentAuthor.name === item.name }"
						>
							{{ item.name }}
						</b-dropdown-item>
					</b-dropdown>

					<!-- Sort Dropdown -->
					<b-dropdown v-model="currentSort" aria-role="list" class="sort-dropdown">
						<template #trigger="{ active }">
							<button class="filter-btn">
								<i class="mdi mdi-sort-variant"></i>
								<span>{{ currentSort.name }}</span>
								<i :class="'mdi ' + (active ? 'mdi-chevron-up' : 'mdi-chevron-down')"></i>
							</button>
						</template>
						<b-dropdown-item
							v-for="item in sortMenu"
							:key="item.name"
							:value="item"
							:class="{ 'is-active': currentSort.name === item.name }"
						>
							{{ item.name }}
						</b-dropdown-item>
					</b-dropdown>

					<!-- Refresh Button -->
					<button class="icon-btn refresh-btn" :class="{ 'is-spinning': isLoading }" :title="$t('Refresh Store')" @click="refreshStore">
						<i class="mdi mdi-refresh"></i>
					</button>
				</div>
			</header>

			<!-- Scrollable Content Body -->
			<div class="main-body">
				<!-- Discover Tab: Featured Hero Carousel & Curated Categories -->
				<section v-if="activeTab === 'discover' && !searchQuery" class="discover-section">
					<!-- Hero Swiper Carousel -->
					<div v-if="recommendList.length > 0" class="hero-carousel-wrapper">
						<div class="hero-carousel">
							<div
								v-for="(item, idx) in featuredList"
								:key="'feat-' + item.id"
								class="hero-slide"
								:class="{ 'is-active': currentHeroIndex === idx }"
							>
								<div class="hero-ambient-glow"></div>

								<div class="hero-content">
									<div class="hero-badge">
										<i class="mdi mdi-star"></i>
										<span>{{ $t('FEATURED APPLICATION') }}</span>
									</div>
									<div class="hero-app-info">
										<img :src="item.icon" class="hero-app-icon" :alt="item.title" @error="onIconError" />
										<div class="hero-text-col">
											<h3 class="hero-app-title">{{ item.title }}</h3>
											<span class="hero-app-meta">{{ item.category }} • {{ (item.architectures || ['amd64']).join(', ') }}</span>
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
										<button class="hero-details-btn" @click="showAppDetail(item.id)">
											<span>{{ $t('Details') }}</span>
											<i class="mdi mdi-chevron-right"></i>
										</button>
									</div>
								</div>

								<div class="hero-preview-box" @click="showAppDetail(item.id)">
									<img
										:src="item.thumbnail || item.screenshots[0] || item.icon"
										class="hero-preview-img"
										:alt="item.title"
										@error="onBannerError($event, item)"
									/>
								</div>
							</div>

							<button class="carousel-arrow is-prev" @click="prevHero" :title="$t('Previous')">
								<i class="mdi mdi-chevron-left"></i>
							</button>
							<button class="carousel-arrow is-next" @click="nextHero" :title="$t('Next')">
								<i class="mdi mdi-chevron-right"></i>
							</button>

							<div class="carousel-dots" v-if="featuredList.length > 1">
								<button
									v-for="(_, idx) in featuredList"
									:key="'dot-' + idx"
									class="carousel-dot"
									:class="{ 'is-active': currentHeroIndex === idx }"
									@click="currentHeroIndex = idx"
								></button>
							</div>
						</div>
					</div>

					<!-- Spotlight Picks -->
					<div v-if="recommendList.length > 0" class="section-block">
						<div class="section-header">
							<div>
								<h3 class="section-title">{{ $t('Spotlight & Editors’ Picks') }}</h3>
								<span class="section-subtitle">{{ $t('Curated essential self-hosted applications for your home server') }}</span>
							</div>
						</div>
						<div class="app-grid">
							<div
								v-for="item in recommendList.slice(0, 4)"
								:key="'spotlight-' + item.id"
								class="app-card"
								@click="showAppDetail(item.id)"
							>
								<div class="card-banner">
									<img
										v-if="item.thumbnail || item.screenshots[0]"
										:src="item.thumbnail || item.screenshots[0]"
										class="card-banner-img"
										:alt="item.title"
										loading="lazy"
										@error="onBannerError($event, item)"
									/>
									<div v-else class="card-banner-placeholder" :style="getGradientBg(item.title)">
										<i :class="'mdi mdi-' + getCateIcon(item.category) + ' placeholder-icon'"></i>
									</div>
								</div>

								<div class="app-card-body">
									<div class="app-card-top">
										<img :src="item.icon" class="app-icon" :alt="item.title" @error="onIconError" />
										<div class="app-info">
											<h4 class="app-title">{{ item.title }}</h4>
											<span class="app-author">{{ item.author || item.developer || 'Community' }}</span>
										</div>
									</div>
									<p class="app-tagline">{{ item.tagline }}</p>
									<div class="app-card-bottom">
										<div class="app-meta-group">
											<span class="app-cat-pill">{{ item.category }}</span>
											<span class="app-arch-text">{{ (item.architectures || ['amd64']).join(', ') }}</span>
										</div>
										<div class="app-card-action" @click.stop>
											<button
												v-if="installedList.includes(item.id)"
												class="card-btn is-open"
												@click="openThirdContainerByAppInfo(item)"
											>
												{{ $t('Open') }}
											</button>
											<div v-else class="card-btn-split">
												<button
													class="card-btn is-install"
													:disabled="!isArchCompatible(item) || isAppInstalling(item.id)"
													:class="{ 'is-loading': isAppInstalling(item.id) }"
													@click="installApp(item.id, item)"
												>
													<span>{{ getInstallButtonText(item.id) }}</span>
												</button>
												<button
													class="card-btn-cog"
													:disabled="!isArchCompatible(item)"
													:title="$t('Customize & Install')"
													@click="openCustomizeForApp(item.id, item)"
												>
													<i class="mdi mdi-tune-variant"></i>
												</button>
											</div>
										</div>
									</div>
								</div>
							</div>
						</div>
					</div>

					<!-- Section: Media & Streaming -->
					<div v-if="mediaApps.length > 0" class="section-block">
						<div class="section-header">
							<div>
								<h3 class="section-title">{{ $t('Media & Entertainment') }}</h3>
								<span class="section-subtitle">{{ $t('Stream movies, music, audiobooks, and organize your collection') }}</span>
							</div>
							<button class="see-all-btn" @click="selectCategoryByName('Media')">
								<span>{{ $t('See All') }}</span>
								<i class="mdi mdi-chevron-right"></i>
							</button>
						</div>
						<div class="app-grid">
							<div
								v-for="item in mediaApps"
								:key="'media-' + item.id"
								class="app-card"
								@click="showAppDetail(item.id)"
							>
								<div class="card-banner">
									<img
										v-if="item.thumbnail || item.screenshots[0]"
										:src="item.thumbnail || item.screenshots[0]"
										class="card-banner-img"
										:alt="item.title"
										loading="lazy"
										@error="onBannerError($event, item)"
									/>
									<div v-else class="card-banner-placeholder" :style="getGradientBg(item.title)">
										<i :class="'mdi mdi-' + getCateIcon(item.category) + ' placeholder-icon'"></i>
									</div>
								</div>

								<div class="app-card-body">
									<div class="app-card-top">
										<img :src="item.icon" class="app-icon" :alt="item.title" @error="onIconError" />
										<div class="app-info">
											<h4 class="app-title">{{ item.title }}</h4>
											<span class="app-author">{{ item.author || item.developer || 'Community' }}</span>
										</div>
									</div>
									<p class="app-tagline">{{ item.tagline }}</p>
									<div class="app-card-bottom">
										<div class="app-meta-group">
											<span class="app-cat-pill">{{ item.category }}</span>
											<span class="app-arch-text">{{ (item.architectures || ['amd64']).join(', ') }}</span>
										</div>
										<div class="app-card-action" @click.stop>
											<button
												v-if="installedList.includes(item.id)"
												class="card-btn is-open"
												@click="openThirdContainerByAppInfo(item)"
											>
												{{ $t('Open') }}
											</button>
											<div v-else class="card-btn-split">
												<button
													class="card-btn is-install"
													:disabled="!isArchCompatible(item) || isAppInstalling(item.id)"
													:class="{ 'is-loading': isAppInstalling(item.id) }"
													@click="installApp(item.id, item)"
												>
													<span>{{ getInstallButtonText(item.id) }}</span>
												</button>
												<button
													class="card-btn-cog"
													:disabled="!isArchCompatible(item)"
													:title="$t('Customize & Install')"
													@click="openCustomizeForApp(item.id, item)"
												>
													<i class="mdi mdi-tune-variant"></i>
												</button>
											</div>
										</div>
									</div>
								</div>
							</div>
						</div>
					</div>

					<!-- Section: AI & LLMs -->
					<div v-if="aiApps.length > 0" class="section-block">
						<div class="section-header">
							<div>
								<h3 class="section-title">{{ $t('AI & Next-Gen LLMs') }}</h3>
								<span class="section-subtitle">{{ $t('Run open-source large language models, ChatGPT clones, and AI agents privately') }}</span>
							</div>
							<button class="see-all-btn" @click="selectCategoryByName('AI')">
								<span>{{ $t('See All') }}</span>
								<i class="mdi mdi-chevron-right"></i>
							</button>
						</div>
						<div class="app-grid">
							<div
								v-for="item in aiApps"
								:key="'ai-' + item.id"
								class="app-card"
								@click="showAppDetail(item.id)"
							>
								<div class="card-banner">
									<img
										v-if="item.thumbnail || item.screenshots[0]"
										:src="item.thumbnail || item.screenshots[0]"
										class="card-banner-img"
										:alt="item.title"
										loading="lazy"
										@error="onBannerError($event, item)"
									/>
									<div v-else class="card-banner-placeholder" :style="getGradientBg(item.title)">
										<i :class="'mdi mdi-' + getCateIcon(item.category) + ' placeholder-icon'"></i>
									</div>
								</div>

								<div class="app-card-body">
									<div class="app-card-top">
										<img :src="item.icon" class="app-icon" :alt="item.title" @error="onIconError" />
										<div class="app-info">
											<h4 class="app-title">{{ item.title }}</h4>
											<span class="app-author">{{ item.author || item.developer || 'Community' }}</span>
										</div>
									</div>
									<p class="app-tagline">{{ item.tagline }}</p>
									<div class="app-card-bottom">
										<div class="app-meta-group">
											<span class="app-cat-pill">{{ item.category }}</span>
											<span class="app-arch-text">{{ (item.architectures || ['amd64']).join(', ') }}</span>
										</div>
										<div class="app-card-action" @click.stop>
											<button
												v-if="installedList.includes(item.id)"
												class="card-btn is-open"
												@click="openThirdContainerByAppInfo(item)"
											>
												{{ $t('Open') }}
											</button>
											<div v-else class="card-btn-split">
												<button
													class="card-btn is-install"
													:disabled="!isArchCompatible(item) || isAppInstalling(item.id)"
													:class="{ 'is-loading': isAppInstalling(item.id) }"
													@click="installApp(item.id, item)"
												>
													<span>{{ getInstallButtonText(item.id) }}</span>
												</button>
												<button
													class="card-btn-cog"
													:disabled="!isArchCompatible(item)"
													:title="$t('Customize & Install')"
													@click="openCustomizeForApp(item.id, item)"
												>
													<i class="mdi mdi-tune-variant"></i>
												</button>
											</div>
										</div>
									</div>
								</div>
							</div>
						</div>
					</div>

					<!-- Section: Developer Tools -->
					<div v-if="devApps.length > 0" class="section-block">
						<div class="section-header">
							<div>
								<h3 class="section-title">{{ $t('Developer & DevOps Tools') }}</h3>
								<span class="section-subtitle">{{ $t('Code editors, databases, container managers, and automation suites') }}</span>
							</div>
							<button class="see-all-btn" @click="selectCategoryByName('Developer')">
								<span>{{ $t('See All') }}</span>
								<i class="mdi mdi-chevron-right"></i>
							</button>
						</div>
						<div class="app-grid">
							<div
								v-for="item in devApps"
								:key="'dev-' + item.id"
								class="app-card"
								@click="showAppDetail(item.id)"
							>
								<div class="card-banner">
									<img
										v-if="item.thumbnail || item.screenshots[0]"
										:src="item.thumbnail || item.screenshots[0]"
										class="card-banner-img"
										:alt="item.title"
										loading="lazy"
										@error="onBannerError($event, item)"
									/>
									<div v-else class="card-banner-placeholder" :style="getGradientBg(item.title)">
										<i :class="'mdi mdi-' + getCateIcon(item.category) + ' placeholder-icon'"></i>
									</div>
								</div>

								<div class="app-card-body">
									<div class="app-card-top">
										<img :src="item.icon" class="app-icon" :alt="item.title" @error="onIconError" />
										<div class="app-info">
											<h4 class="app-title">{{ item.title }}</h4>
											<span class="app-author">{{ item.author || item.developer || 'Community' }}</span>
										</div>
									</div>
									<p class="app-tagline">{{ item.tagline }}</p>
									<div class="app-card-bottom">
										<div class="app-meta-group">
											<span class="app-cat-pill">{{ item.category }}</span>
											<span class="app-arch-text">{{ (item.architectures || ['amd64']).join(', ') }}</span>
										</div>
										<div class="app-card-action" @click.stop>
											<button
												v-if="installedList.includes(item.id)"
												class="card-btn is-open"
												@click="openThirdContainerByAppInfo(item)"
											>
												{{ $t('Open') }}
											</button>
											<div v-else class="card-btn-split">
												<button
													class="card-btn is-install"
													:disabled="!isArchCompatible(item) || isAppInstalling(item.id)"
													:class="{ 'is-loading': isAppInstalling(item.id) }"
													@click="installApp(item.id, item)"
												>
													<span>{{ getInstallButtonText(item.id) }}</span>
												</button>
												<button
													class="card-btn-cog"
													:disabled="!isArchCompatible(item)"
													:title="$t('Customize & Install')"
													@click="openCustomizeForApp(item.id, item)"
												>
													<i class="mdi mdi-tune-variant"></i>
												</button>
											</div>
										</div>
									</div>
								</div>
							</div>
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
						<div
							v-for="item in displayAppsList"
							:key="item.id"
							class="app-card"
							@click="showAppDetail(item.id)"
						>
							<div class="card-banner">
								<img
									v-if="item.thumbnail || item.screenshots[0]"
									:src="item.thumbnail || item.screenshots[0]"
									class="card-banner-img"
									:alt="item.title"
									loading="lazy"
									@error="onBannerError($event, item)"
								/>
								<div v-else class="card-banner-placeholder" :style="getGradientBg(item.title)">
									<i :class="'mdi mdi-' + getCateIcon(item.category) + ' placeholder-icon'"></i>
								</div>
							</div>

							<div class="app-card-body">
								<div class="app-card-top">
									<img :src="item.icon" class="app-icon" :alt="item.title" @error="onIconError" />
									<div class="app-info">
										<h4 class="app-title">{{ item.title }}</h4>
										<span class="app-author">{{ item.author || item.developer || 'Community' }}</span>
									</div>
								</div>
								<p class="app-tagline">{{ item.tagline }}</p>
								<div class="app-card-bottom">
									<div class="app-meta-group">
										<span class="app-cat-pill">{{ item.category }}</span>
										<span class="app-arch-text">{{ (item.architectures || ['amd64']).join(', ') }}</span>
									</div>
									<div class="app-card-action" @click.stop>
										<button
											v-if="installedList.includes(item.id)"
											class="card-btn is-open"
											@click="openThirdContainerByAppInfo(item)"
										>
											{{ $t('Open') }}
										</button>
										<div v-else class="card-btn-split">
											<button
												class="card-btn is-install"
												:disabled="!isArchCompatible(item) || isAppInstalling(item.id)"
												:class="{ 'is-loading': isAppInstalling(item.id) }"
												@click="installApp(item.id, item)"
											>
												<span>{{ getInstallButtonText(item.id) }}</span>
											</button>
											<button
												class="card-btn-cog"
												:disabled="!isArchCompatible(item)"
												:title="$t('Customize & Install')"
												@click="openCustomizeForApp(item.id, item)"
											>
												<i class="mdi mdi-tune-variant"></i>
											</button>
										</div>
									</div>
								</div>
							</div>
						</div>
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
						<img :src="formState.icon || defaultAppIcon" class="installer-app-icon" @error="onFormIconError" />
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
								<input v-model="formState.title" type="text" class="form-input" :placeholder="$t('e.g., Nextcloud')" />
							</div>
							<div class="form-group">
								<label class="form-label">{{ $t('Container Name') }} <span class="req">*</span></label>
								<input v-model="formState.containerName" type="text" class="form-input" :placeholder="$t('e.g., nextcloud')" />
							</div>
						</div>

						<div class="form-grid-2 mt-3">
							<div class="form-group">
								<label class="form-label">{{ $t('Docker Image') }} <span class="req">*</span></label>
								<div class="input-with-tags">
									<input v-model="formState.image" type="text" class="form-input" :placeholder="$t('e.g., nextcloud:latest')" />
								</div>
								<div class="quick-tags">
									<span class="quick-tag-label">{{ $t('Quick Tags:') }}</span>
									<button class="quick-tag-btn" @click="appendImageTag('latest')">:latest</button>
									<button class="quick-tag-btn" @click="appendImageTag('alpine')">:alpine</button>
									<button class="quick-tag-btn" @click="appendImageTag('stable')">:stable</button>
								</div>
							</div>

							<div class="form-group">
								<label class="form-label">{{ $t('Category') }}</label>
								<select v-model="formState.category" class="form-select">
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
								<img :src="formState.icon || defaultAppIcon" class="icon-preview-thumb" @error="onFormIconError" />
								<input v-model="formState.icon" type="text" class="form-input" :placeholder="$t('https://icon.casaos.io/main/all/app.png')" />
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
								<select v-model="formState.webUI.scheme" class="form-select">
									<option value="http">http://</option>
									<option value="https">https://</option>
								</select>
							</div>
							<div class="form-group">
								<label class="form-label">{{ $t('Port') }}</label>
								<input v-model="formState.webUI.port" type="text" class="form-input" :placeholder="$t('e.g., 80 or 8080')" />
							</div>
							<div class="form-group">
								<label class="form-label">{{ $t('Index Path [Optional]') }}</label>
								<input v-model="formState.webUI.index" type="text" class="form-input" :placeholder="$t('/index.html or /login')" />
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
									<input v-model="p.host" type="text" class="form-input is-sm" placeholder="8080" />
								</div>
								<span class="row-arrow">➔</span>
								<div class="dynamic-field">
									<span class="field-mini-label">{{ $t('Container Port') }}</span>
									<input v-model="p.container" type="text" class="form-input is-sm" placeholder="80" />
								</div>
								<div class="dynamic-field is-protocol">
									<span class="field-mini-label">{{ $t('Protocol') }}</span>
									<select v-model="p.protocol" class="form-select is-sm">
										<option value="TCP">TCP</option>
										<option value="UDP">UDP</option>
									</select>
								</div>
								<button class="delete-row-btn" @click="removePort(pidx)">
									<i class="mdi mdi-delete-outline"></i>
								</button>
							</div>

							<button class="add-row-btn" @click="addPort">
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
									<input v-model="v.host" type="text" class="form-input is-sm" placeholder="/DATA/AppData/app/config" />
								</div>
								<span class="row-arrow">➔</span>
								<div class="dynamic-field is-wide">
									<span class="field-mini-label">{{ $t('Container Path') }}</span>
									<input v-model="v.container" type="text" class="form-input is-sm" placeholder="/config" />
								</div>
								<div class="dynamic-field is-mode">
									<span class="field-mini-label">{{ $t('Mode') }}</span>
									<select v-model="v.mode" class="form-select is-sm">
										<option value="rw">Read / Write (rw)</option>
										<option value="ro">Read-Only (ro)</option>
									</select>
								</div>
								<button class="delete-row-btn" @click="removeVolume(vidx)">
									<i class="mdi mdi-delete-outline"></i>
								</button>
							</div>

							<button class="add-row-btn" @click="addVolume">
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
									<input v-model="e.key" type="text" class="form-input is-sm is-mono" placeholder="TZ" />
								</div>
								<span class="row-arrow">=</span>
								<div class="dynamic-field is-wide">
									<span class="field-mini-label">{{ $t('Value') }}</span>
									<input v-model="e.value" type="text" class="form-input is-sm is-mono" placeholder="UTC" />
								</div>
								<button class="delete-row-btn" @click="removeEnv(eidx)">
									<i class="mdi mdi-delete-outline"></i>
								</button>
							</div>

							<button class="add-row-btn" @click="addEnv">
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
							<button class="accordion-toggle-btn">
								<i :class="'mdi ' + (showAdvanced ? 'mdi-chevron-up' : 'mdi-chevron-down')"></i>
							</button>
						</div>

						<div v-if="showAdvanced" class="advanced-body mt-4">
							<div class="form-grid-3">
								<div class="form-group">
									<label class="form-label">{{ $t('Network Driver') }}</label>
									<select v-model="formState.network" class="form-select">
										<option value="bridge">bridge (Isolated NAT)</option>
										<option value="host">host (Direct Host Networking)</option>
										<option v-for="net in networkOptions" :key="net" :value="net">{{ net }}</option>
									</select>
								</div>

								<div class="form-group">
									<label class="form-label">{{ $t('Restart Policy') }}</label>
									<select v-model="formState.restart" class="form-select">
										<option value="unless-stopped">unless-stopped (Recommended)</option>
										<option value="always">always</option>
										<option value="on-failure">on-failure</option>
										<option value="no">no</option>
									</select>
								</div>

								<div class="form-group">
									<label class="form-label">{{ $t('Memory Limit (MB)') }}</label>
									<input v-model="formState.memoryLimit" type="text" class="form-input" placeholder="0 (Unlimited)" />
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
								<input v-model="formState.command" type="text" class="form-input is-mono" placeholder="e.g., sh -c 'npm start'" />
							</div>

							<!-- Devices Section -->
							<div class="mt-4">
								<label class="form-label">{{ $t('Hardware Device Passthrough') }}</label>
								<div class="dynamic-list">
									<div v-for="(dev, didx) in formState.devices" :key="'dev-' + didx" class="dynamic-row">
										<div class="dynamic-field is-wide">
											<span class="field-mini-label">{{ $t('Host Device') }}</span>
											<input v-model="dev.host" type="text" class="form-input is-sm is-mono" placeholder="/dev/dri" />
										</div>
										<span class="row-arrow">➔</span>
										<div class="dynamic-field is-wide">
											<span class="field-mini-label">{{ $t('Container Device') }}</span>
											<input v-model="dev.container" type="text" class="form-input is-sm is-mono" placeholder="/dev/dri" />
										</div>
										<button class="delete-row-btn" @click="removeDevice(didx)">
											<i class="mdi mdi-delete-outline"></i>
										</button>
									</div>
									<button class="add-row-btn" @click="addDevice">
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
			<footer class="installer-footer">
				<div class="installer-footer-info">
					<i class="mdi mdi-information-outline"></i>
					<span>{{ $t('Docker 20.10+ · Container configurations are fully persistent and managed via NivaroOS Compose.') }}</span>
				</div>
				<div class="installer-footer-btns">
					<button class="installer-cancel-btn" @click="viewMode = 'store'">
						{{ $t('Cancel') }}
					</button>
					<button
						class="installer-deploy-btn"
						:class="{ 'is-loading': isDeployingCustom }"
						:disabled="!isFormValid"
						@click="installFromInstaller"
					>
						<i v-if="!isDeployingCustom" class="mdi mdi-check-circle-outline"></i>
						<span v-if="!isDeployingCustom">{{ formState.isEditing ? $t('Save & Apply Settings') : $t('Install Container') }}</span>
						<span v-else>{{ formState.isEditing ? $t('Applying Settings...') : $t('Deploying Container...') }}</span>
					</button>
				</div>
			</footer>
		</main>

		<!-- In-Window App Detail Drawer -->
		<transition name="drawer-slide">
			<div v-if="selectedAppDetail" class="app-detail-drawer" @click.self="closeAppDetail">
				<div class="drawer-panel">
					<header class="drawer-header">
						<button class="back-btn" @click="closeAppDetail">
							<i class="mdi mdi-arrow-left"></i>
							<span>{{ $t('Back to Store') }}</span>
						</button>
						<div class="drawer-header-spacer"></div>
						<button class="drawer-close-btn" @click="closeAppDetail">
							<i class="mdi mdi-close"></i>
						</button>
					</header>

					<div class="drawer-content">
						<!-- App Hero -->
						<div class="detail-hero">
							<img :src="selectedAppDetail.icon" class="detail-icon" :alt="selectedAppDetail.title" @error="onIconError" />
							<div class="detail-hero-info">
								<h2 class="detail-title">{{ i18n(selectedAppDetail.title) }}</h2>
								<p class="detail-tagline">{{ i18n(selectedAppDetail.tagline) }}</p>
								<div class="detail-meta-row">
									<span v-if="selectedAppDetail.category" class="detail-pill">{{ selectedAppDetail.category }}</span>
									<span v-if="selectedAppDetail.version" class="detail-pill is-subtle">v{{ selectedAppDetail.version }}</span>
									<span v-if="selectedAppDetail.developer" class="detail-pill is-subtle">{{ selectedAppDetail.developer }}</span>
									<span v-if="!isArchCompatible(selectedAppDetail)" class="detail-pill is-danger">{{ $t('Incompatible Architecture') }}</span>
								</div>
								<div class="detail-actions">
									<button
										v-if="installedList.includes(selectedAppDetail.id)"
										class="detail-action-btn is-open"
										@click="openThirdContainerByAppInfo(selectedAppDetail)"
									>
										<i class="mdi mdi-launch"></i>
										<span>{{ $t('Open App') }}</span>
									</button>
									<template v-else>
										<button
											class="detail-action-btn is-install"
											:disabled="!isArchCompatible(selectedAppDetail) || isAppInstalling(selectedAppDetail.id)"
											:class="{ 'is-loading': isAppInstalling(selectedAppDetail.id) }"
											@click="installApp(selectedAppDetail.id, selectedAppDetail)"
										>
											<i v-if="!isAppInstalling(selectedAppDetail.id)" class="mdi mdi-download"></i>
											<span>{{ getInstallButtonText(selectedAppDetail.id) }}</span>
										</button>
										<button
											class="detail-action-btn is-customize"
											:disabled="!isArchCompatible(selectedAppDetail)"
											@click="openCustomizeForApp(selectedAppDetail.id, selectedAppDetail)"
										>
											<i class="mdi mdi-tune-variant"></i>
											<span>{{ $t('Customize & Install') }}</span>
										</button>
									</template>
								</div>
							</div>
						</div>

						<!-- Screenshots Gallery with Lightbox -->
						<div v-if="detailScreenshots.length > 0" class="detail-section">
							<h4 class="detail-section-title">{{ $t('Screenshots & Preview') }}</h4>
							<div class="screenshots-gallery">
								<div
									v-for="(img, sidx) in detailScreenshots"
									:key="'screen-' + sidx"
									class="screenshot-item"
									@click="activeLightboxImage = img"
								>
									<img :src="img" :alt="'Screenshot ' + (sidx + 1)" loading="lazy" />
									<div class="screenshot-hover-overlay">
										<i class="mdi mdi-magnify-plus-outline"></i>
									</div>
								</div>
							</div>
						</div>

						<!-- Description Markdown -->
						<div class="detail-section">
							<h4 class="detail-section-title">{{ $t('About this App') }}</h4>
							<div class="detail-description">
								<p class="description-text">{{ i18n(selectedAppDetail.description) || i18n(selectedAppDetail.tagline) }}</p>
							</div>
						</div>

						<!-- Specifications -->
						<div class="detail-section">
							<h4 class="detail-section-title">{{ $t('Specifications & Details') }}</h4>
							<div class="specs-grid">
								<div class="spec-card">
									<span class="spec-label">{{ $t('Category') }}</span>
									<span class="spec-value">{{ selectedAppDetail.category || 'Utility' }}</span>
								</div>
								<div class="spec-card">
									<span class="spec-label">{{ $t('Memory Required') }}</span>
									<span class="spec-value">{{ selectedAppDetail.min_memory || '256 MB' }}</span>
								</div>
								<div class="spec-card">
									<span class="spec-label">{{ $t('Architectures') }}</span>
									<span class="spec-value">{{ (selectedAppDetail.architectures || ['amd64', 'arm64']).join(', ') }}</span>
								</div>
								<div class="spec-card">
									<span class="spec-label">{{ $t('Developer') }}</span>
									<span class="spec-value">{{ selectedAppDetail.developer || selectedAppDetail.author || 'Community' }}</span>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		</transition>

		<!-- Screenshot Lightbox Modal -->
		<transition name="fade">
			<div v-if="activeLightboxImage" class="lightbox-overlay" @click="activeLightboxImage = null">
				<button class="lightbox-close-btn" @click="activeLightboxImage = null">
					<i class="mdi mdi-close"></i>
				</button>
				<img :src="activeLightboxImage" class="lightbox-img" alt="Enlarged screenshot" @click.stop />
			</div>
		</transition>

		<!-- In-Window Sources Management Modal -->
		<transition name="fade">
			<div v-if="showSourcesModal" class="sources-overlay" @click.self="showSourcesModal = false">
				<div class="sources-panel">
					<header class="sources-header">
						<h3 class="sources-title">{{ $t('App Store Sources') }}</h3>
						<button class="drawer-close-btn" @click="showSourcesModal = false">
							<i class="mdi mdi-close"></i>
						</button>
					</header>
					<div class="sources-body">
						<div class="add-source-box">
							<input
								v-model="newSourceUrl"
								type="text"
								class="source-input"
								:placeholder="$t('Enter GitHub repository zip or store JSON URL...')"
							/>
							<button class="add-source-btn" :disabled="!newSourceUrl.trim()" @click="addStoreSource">
								{{ $t('Add Source') }}
							</button>
						</div>
						<div class="sources-list">
							<div v-for="(src, sidx) in storeSourcesList" :key="'src-' + sidx" class="source-row">
								<div class="source-info">
									<span class="source-name">{{ src.name || src.url || src }}</span>
								</div>
								<button class="delete-source-btn" @click="removeStoreSource(src)">
									<i class="mdi mdi-delete-outline"></i>
								</button>
							</div>
						</div>
					</div>
					<footer class="sources-footer">
						<button class="footer-done-btn" @click="showSourcesModal = false">{{ $t('Done') }}</button>
					</footer>
				</div>
			</div>
		</transition>
	</div>
</template>

<script>
import appStoreIcon from '@/assets/img/app/appstore.png'
import defaultAppIcon from '@/assets/img/app/default.svg'
import business_OpenThirdApp from '@/mixins/app/Business_OpenThirdApp'
import business_ShowNewAppTag from '@/mixins/app/Business_ShowNewAppTag'
import { ice_i18n } from '@/mixins/base/common-i18n'
import debounce from 'lodash/debounce'
import YAML from 'yaml'
import FileSaver from 'file-saver'
import copy from 'clipboard-copy'

const ARCH_MAP = {
	x86_64: 'amd64',
	aarch64: 'arm64',
	armv7l: 'armv7'
}

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
	mixins: [business_OpenThirdApp, business_ShowNewAppTag],
	props: {
		storeId: {
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
			sortMenu: [
				{ name: 'Popularity' },
				{ name: 'Name (A-Z)' },
				{ name: 'Newest' }
			],
			currentSort: { name: 'Popularity' },
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
			resizeObserver: null
		}
	},
	computed: {
		isCompact() {
			return this.width < 768
		},
		isNarrow() {
			return this.width < 540
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
			return Boolean(this.formState.title && this.formState.image && this.formState.containerName)
		},
		arch() {
			const rawArch = this.$store.state.hardwareInfo?.cpu?.arch || 'x86_64'
			return ARCH_MAP[rawArch] || rawArch
		},
		featuredList() {
			return this.recommendList.slice(0, 6)
		},
		mediaApps() {
			return this.allAppsList.filter(item => item.category === 'Media').slice(0, 6)
		},
		aiApps() {
			return this.allAppsList.filter(item => item.category === 'AI').slice(0, 6)
		},
		devApps() {
			return this.allAppsList.filter(item => item.category === 'Developer').slice(0, 6)
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
				list = list.filter(item => item.author === 'CasaOS' || item.author === 'Official' || item.author === 'IceWhale' || item.author === 'ZimaOS Team')
			} else if (this.currentAuthor.name === 'Community') {
				list = list.filter(item => item.author !== 'CasaOS' && item.author !== 'Official' && item.author !== 'IceWhale' && item.author !== 'ZimaOS Team')
			}

			if (this.debouncedSearch) {
				const q = this.debouncedSearch.toLowerCase()
				list = list.filter(item => {
					const title = (item.title || '').toLowerCase()
					const tag = (item.tagline || '').toLowerCase()
					const cat = (item.category || '').toLowerCase()
					return title.includes(q) || tag.includes(q) || cat.includes(q)
				})
			}

			if (this.currentSort.name === 'Name (A-Z)') {
				list.sort((a, b) => (a.title || '').localeCompare(b.title || ''))
			}

			return list
		},
		detailScreenshots() {
			if (!this.selectedAppDetail) return []
			const links = this.selectedAppDetail.screenshot_link || []
			return Array.isArray(links) ? links.filter(Boolean) : [links]
		}
	},
	watch: {
		formState: {
			handler() {
				if (this.viewMode === 'installer' && this.installerTab === 'form') {
					this.customComposeYaml = this.formDataToYaml(this.formState)
				}
			},
			deep: true
		}
	},
	created() {
		this.onSearchInput = debounce(() => {
			this.debouncedSearch = this.searchQuery
			if (this.searchQuery && this.activeTab === 'discover') {
				this.activeTab = 'all'
			}
		}, 250)
	},
	async mounted() {
		this.resizeObserver = new ResizeObserver(entries => {
			if (entries && entries[0]) {
				this.width = entries[0].contentRect.width
			}
		})
		this.resizeObserver.observe(this.$el)

		await Promise.all([
			this.initStore(),
			this.fetchNetworks()
		])

		this.startHeroAutoplay()

		if (this.initialMode === 'edit' && this.initialAppName) {
			this.openEditForInstalledApp(this.initialAppName)
		} else if (this.initialMode === 'custom') {
			this.openCustomInstall()
		}
	},
	beforeDestroy() {
		if (this.resizeObserver) this.resizeObserver.disconnect()
		if (this.heroTimer) clearInterval(this.heroTimer)
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
			if (n.includes('ai') || n.includes('llm') || n.includes('gpt')) return 'robot-outline'
			if (n.includes('dev') || n.includes('code') || n.includes('it')) return 'code-tags'
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
		startHeroAutoplay() {
			if (this.heroTimer) clearInterval(this.heroTimer)
			this.heroTimer = setInterval(() => {
				if (this.featuredList.length > 1 && this.viewMode === 'store') {
					this.nextHero()
				}
			}, 7000)
		},
		nextHero() {
			this.currentHeroIndex = (this.currentHeroIndex + 1) % this.featuredList.length
		},
		prevHero() {
			this.currentHeroIndex = (this.currentHeroIndex - 1 + this.featuredList.length) % this.featuredList.length
		},
		async initStore() {
			this.isLoading = true
			try {
				await Promise.all([
					this.fetchCategories(),
					this.fetchRecommend(),
					this.fetchStoreList(),
					this.fetchSources()
				])
			} catch (e) {
				console.error('Failed to init app store', e)
			} finally {
				this.isLoading = false
			}
		},
		async fetchNetworks() {
			try {
				const res = await this.$openAPI.appManagement.compose.dockerNetworks()
				if (res && res.data && res.data.data) {
					this.networks = res.data.data
				}
			} catch (e) {
				console.warn('Failed to load docker networks', e)
			}
		},
		async fetchCategories() {
			try {
				const res = await this.$openAPI.appManagement.appStore.categoryList()
				if (res && res.data && res.data.data) {
					this.cateMenu = res.data.data.filter(c => c.count > 0)
				}
			} catch (e) {
				console.warn('Failed to load categories', e)
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
				console.warn('Failed to load recommend apps', e)
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
				console.warn('Failed to load store list', e)
			}
		},
		async fetchSources() {
			try {
				const res = await this.$openAPI.appManagement.appStore.appStoreList()
				this.storeSourcesList = res.data?.data || []
			} catch (e) {
				console.warn('Failed to load sources', e)
			}
		},
		async refreshStore() {
			await this.initStore()
			this.$buefy.toast.open({
				message: this.$t('App store catalog refreshed'),
				type: 'is-success',
				position: 'is-top',
				duration: 2000
			})
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
		},
		async showAppDetail(id) {
			try {
				const res = await this.$openAPI.appManagement.appStore.composeAppStoreInfo(id)
				if (res && res.data && res.data.data) {
					this.selectedAppDetail = {
						id,
						...res.data.data
					}
				}
			} catch (e) {
				console.error('Failed to get app details', e)
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
						this.$buefy.modal.open({
							parent: this,
							component: () => import('@/components/Apps/TipEditorModal.vue'),
							hasModalCard: true,
							trapFocus: true,
							canCancel: [''],
							scroll: 'keep',
							animation: 'zoom-in',
							events: {
								submit: async () => {
									await this.executeInstall(res.data, item || { title: id, id })
								}
							},
							props: {
								composeData: composeJSON
							}
						})
					} else {
						await this.executeInstall(res.data, item || { title: id, id })
					}
				} else {
					throw new Error(this.$t('Failed to fetch application compose configuration'))
				}
			} catch (e) {
				this.$delete(this.installingMap, id)
				this.$buefy.toast.open({
					message: this.$t('Installation failed') + ': ' + (e.response?.data?.message || e.message || e),
					type: 'is-danger',
					position: 'is-top',
					duration: 4000
				})
			}
		},
		async executeInstall(yamlData, item) {
			try {
				if (this.$messageBus) {
					this.$messageBus('appstore_install', item?.title || '')
				}
				const installRes = await this.$openAPI.appManagement.compose.installComposeApp(yamlData, false, true)
				if (installRes.status === 200) {
					this.$buefy.toast.open({
						message: this.$t('Installation started for {title}', { title: item?.title || 'app' }),
						type: 'is-success',
						position: 'is-top',
						duration: 3000
					})
				} else {
					this.$delete(this.installingMap, item.id)
					this.$buefy.toast.open({
						message: installRes.data?.message || this.$t('Installation failed'),
						type: 'is-warning',
						position: 'is-top',
						duration: 4000
					})
				}
			} catch (e) {
				this.$delete(this.installingMap, item.id)
				this.$buefy.toast.open({
					message: this.$t('Installation failed') + ': ' + (e.response?.data?.message || e.message || e),
					type: 'is-danger',
					position: 'is-top',
					duration: 4000
				})
			}
		},
		/* Custom Modern Container Studio Methods */
		openCustomInstall() {
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
				this.$buefy.toast.open({
					message: this.$t('Failed to load container configuration') + ': ' + (e.response?.data?.message || e.message),
					type: 'is-danger',
					position: 'is-top'
				})
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
				this.$buefy.toast.open({
					message: this.$t('Failed to load app compose configuration') + ': ' + (e.response?.data?.message || e.message),
					type: 'is-danger',
					position: 'is-top'
				})
			} finally {
				this.isLoading = false
			}
		},
		switchInstallerTab(tab) {
			if (tab === 'yaml') {
				this.customComposeYaml = this.formDataToYaml(this.formState)
			} else if (tab === 'form') {
				const parsed = this.yamlToFormData(this.customComposeYaml)
				if (parsed) {
					parsed.isEditing = this.formState.isEditing
					this.formState = parsed
				}
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
			this.$buefy.modal.open({
				parent: this,
				component: () => import('@/components/forms/ImportPanel.vue'),
				hasModalCard: true,
				customClass: 'import-panel-modal',
				trapFocus: true,
				canCancel: ['escape', 'x', 'outside'],
				events: {
					importInsert: (yaml) => {
						this.customComposeYaml = yaml
						const parsed = this.yamlToFormData(yaml)
						if (parsed) {
							this.formState = parsed
						}
					}
				}
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
					this.$buefy.toast.open({
						message: this.formState.isEditing ? this.$t('Container settings saved successfully!') : this.$t('Application installation started!'),
						type: 'is-success',
						position: 'is-top',
						duration: 4000
					})
					this.viewMode = 'store'
					this.activeTab = 'installed'
					setTimeout(() => {
						this.fetchStoreList()
					}, 4000)
				} else {
					this.$buefy.toast.open({
						message: res.data?.message || this.$t('Operation failed'),
						type: 'is-warning',
						position: 'is-top',
						duration: 5000
					})
				}
			} catch (e) {
				this.$buefy.toast.open({
					message: this.$t('Error') + ': ' + (e.response?.data?.message || e.message || e),
					type: 'is-danger',
					position: 'is-top',
					duration: 5000
				})
			} finally {
				this.isDeployingCustom = false
			}
		},
		yamlToFormData(yamlStr) {
			try {
				const doc = YAML.parse(yamlStr) || {}
				const services = doc.services || {}
				const mainKey = doc['x-casaos']?.main || Object.keys(services)[0] || 'app'
				const service = services[mainKey] || {}

				const titleObj = doc['x-casaos']?.title
				const title = (typeof titleObj === 'object' ? (titleObj.custom || titleObj.en_us || titleObj['en-US']) : titleObj) || doc.name || mainKey
				const icon = doc['x-casaos']?.icon || ''
				const category = doc['x-casaos']?.category || 'Others'
				const tagline = doc['x-casaos']?.tagline?.en_us || ''
				const description = doc['x-casaos']?.description?.en_us || ''

				const rawPorts = service.ports || []
				const ports = rawPorts.map(p => {
					if (typeof p === 'string' || typeof p === 'number') {
						const parts = String(p).split('/')
						const proto = (parts[1] || 'TCP').toUpperCase()
						const hostAndCont = parts[0].split(':')
						if (hostAndCont.length >= 2) {
							return { host: hostAndCont[0], container: hostAndCont[1], protocol: proto }
						} else {
							return { host: hostAndCont[0], container: hostAndCont[0], protocol: proto }
						}
					} else if (p && typeof p === 'object') {
						return { host: String(p.published || p.target), container: String(p.target), protocol: (p.protocol || 'TCP').toUpperCase() }
					}
					return { host: '', container: '', protocol: 'TCP' }
				}).filter(p => p.container)

				const rawVols = service.volumes || []
				const volumes = rawVols.map(v => {
					if (typeof v === 'string') {
						const parts = v.split(':')
						return { host: parts[0] || '', container: parts[1] || '', mode: parts[2] || 'rw' }
					} else if (v && typeof v === 'object') {
						return { host: v.source || '', container: v.target || '', mode: v.read_only ? 'ro' : 'rw' }
					}
					return { host: '', container: '', mode: 'rw' }
				}).filter(v => v.container)

				const rawEnvs = service.environment || []
				const envs = []
				if (Array.isArray(rawEnvs)) {
					rawEnvs.forEach(e => {
						const eq = e.indexOf('=')
						if (eq > -1) {
							envs.push({ key: e.slice(0, eq), value: e.slice(eq + 1) })
						} else {
							envs.push({ key: e, value: '' })
						}
					})
				} else if (rawEnvs && typeof rawEnvs === 'object') {
					Object.entries(rawEnvs).forEach(([k, val]) => {
						envs.push({ key: k, value: String(val) })
					})
				}

				const rawDevs = service.devices || []
				const devices = rawDevs.map(d => {
					if (typeof d === 'string') {
						const parts = d.split(':')
						return { host: parts[0] || '', container: parts[1] || parts[0] || '' }
					}
					return { host: '', container: '' }
				}).filter(d => d.host)

				const scheme = doc['x-casaos']?.scheme || 'http'
				const portMap = doc['x-casaos']?.port_map || (ports[0]?.container || '')
				const index = doc['x-casaos']?.index || ''

				return {
					appName: doc.name || mainKey,
					mainService: mainKey,
					title: title,
					icon: icon,
					category: category,
					tagline: tagline,
					description: description,
					image: service.image || '',
					containerName: service.container_name || doc.name || mainKey,
					webUI: {
						enabled: Boolean(portMap),
						scheme: scheme,
						port: portMap,
						index: index
					},
					ports: ports,
					volumes: volumes,
					envs: envs,
					devices: devices,
					network: service.network_mode || (service.networks ? service.networks[0] : 'bridge') || 'bridge',
					restart: service.restart || 'unless-stopped',
					privileged: Boolean(service.privileged),
					command: Array.isArray(service.command) ? service.command.join(' ') : (service.command || ''),
					memoryLimit: service.deploy?.resources?.limits?.memory || ''
				}
			} catch (e) {
				console.error('Failed to parse compose YAML to form', e)
				return null
			}
		},
		formDataToYaml(form) {
			const mainKey = form.mainService || form.appName || 'app'
			const safeName = (form.appName || form.containerName || 'custom-app').toLowerCase().replace(/[^a-z0-9_-]/g, '-')

			const service = {
				image: form.image || 'nginx:latest',
				container_name: form.containerName || safeName,
				restart: form.restart || 'unless-stopped'
			}

			if (form.network === 'host') {
				service.network_mode = 'host'
			} else if (form.network && form.network !== 'bridge') {
				service.networks = [form.network]
			}

			if (form.ports && form.ports.length > 0 && form.network !== 'host') {
				service.ports = form.ports
					.filter(p => p.container)
					.map(p => {
						const proto = p.protocol && p.protocol !== 'TCP' ? `/${p.protocol.toLowerCase()}` : ''
						if (p.host) {
							return `${p.host}:${p.container}${proto}`
						}
						return `${p.container}${proto}`
					})
			}

			if (form.volumes && form.volumes.length > 0) {
				service.volumes = form.volumes
					.filter(v => v.container)
					.map(v => {
						const mode = v.mode ? `:${v.mode}` : ''
						return `${v.host || '/DATA/AppData/' + safeName}:${v.container}${mode}`
					})
			}

			if (form.envs && form.envs.length > 0) {
				service.environment = form.envs
					.filter(e => e.key)
					.map(e => `${e.key}=${e.value || ''}`)
			}

			if (form.devices && form.devices.length > 0) {
				service.devices = form.devices
					.filter(d => d.host)
					.map(d => `${d.host}:${d.container || d.host}`)
			}

			if (form.privileged) {
				service.privileged = true
			}

			if (form.command && form.command.trim()) {
				service.command = form.command.trim()
			}

			if (form.memoryLimit && String(form.memoryLimit).trim() && String(form.memoryLimit) !== '0') {
				const mem = isNaN(form.memoryLimit) ? form.memoryLimit : `${form.memoryLimit}M`
				service.deploy = {
					resources: {
						limits: {
							memory: mem
						}
					}
				}
			}

			const doc = {
				name: safeName,
				services: {
					[mainKey]: service
				},
				'x-casaos': {
					architectures: ['amd64', 'arm64'],
					main: mainKey,
					title: {
						custom: form.title || safeName,
						en_us: form.title || safeName
					},
					icon: form.icon || 'https://icon.casaos.io/main/all/default.png',
					tagline: {
						en_us: form.tagline || ''
					},
					description: {
						en_us: form.description || ''
					},
					category: form.category || 'Others',
					port_map: form.webUI?.enabled ? String(form.webUI.port || '') : '',
					scheme: form.webUI?.scheme || 'http',
					index: form.webUI?.index || ''
				}
			}

			return YAML.stringify(doc)
		},
		async addStoreSource() {
			if (!this.newSourceUrl.trim()) return
			try {
				await this.$openAPI.appManagement.appStore.registerAppStore(this.newSourceUrl.trim())
				this.newSourceUrl = ''
				await this.fetchSources()
				await this.refreshStore()
			} catch (e) {
				this.$buefy.toast.open({
					message: this.$t('Failed to add store source') + ': ' + (e.message || e),
					type: 'is-danger',
					position: 'is-top'
				})
			}
		},
		async removeStoreSource(src) {
			const url = src.url || src
			try {
				await this.$openAPI.appManagement.appStore.unregisterAppStore(url)
				await this.fetchSources()
				await this.refreshStore()
			} catch (e) {
				console.error('Failed to remove store source', e)
			}
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
			if (name) {
				this.$delete(this.installingMap, name)
			}
			this.fetchStoreList()
		},
		'app:install-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name
			if (name) {
				this.$delete(this.installingMap, name)
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.appstore-app {
	display: flex;
	height: 100%;
	width: 100%;
	background: #f8fafc;
	color: #0f172a;
	font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
	position: relative;
	overflow: hidden;
}

/* Left Sidebar */
.appstore-sidebar {
	width: 240px;
	min-width: 240px;
	background: #ffffff;
	border-right: 1px solid #e2e8f0;
	display: flex;
	flex-direction: column;
	padding: 1rem 0.75rem;
	user-select: none;
}

.sidebar-brand {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	padding: 0.25rem 0.5rem 1rem;
	border-bottom: 1px solid #f1f5f9;
	margin-bottom: 0.625rem;
}

.brand-icon {
	width: 32px;
	height: 32px;
	filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.05));
}

.brand-title {
	font-size: 0.9375rem;
	font-weight: 700;
	line-height: 1.2;
	color: #1e293b;
}

.brand-subtitle {
	font-size: 0.6875rem;
	font-weight: 400;
	color: #94a3b8;
}

.sidebar-nav {
	flex: 1;
	overflow-y: auto;
	padding-right: 0.15rem;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;

	&::-webkit-scrollbar {
		width: 3px;
	}
	&::-webkit-scrollbar-thumb {
		background: #cbd5e1;
		border-radius: 2px;
	}
}

.nav-item {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	width: 100%;
	padding: 0.45rem 0.65rem;
	border-radius: 0.375rem;
	border: none;
	background: transparent;
	color: #475569;
	font-size: 0.8125rem;
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
		color: #64748b;
		transition: color 0.15s ease;

		i.mdi {
			font-size: 15px;
			line-height: 1;
			display: inline-block;
			text-rendering: geometricPrecision;
			-webkit-font-smoothing: antialiased;
			-moz-osx-font-smoothing: grayscale;
		}
	}

	&:hover {
		background: #f1f5f9;
		color: #0f172a;

		.nav-icon {
			color: #1e293b;
		}
	}

	&.is-active {
		background: #2563eb;
		color: #ffffff;
		font-weight: 600;

		.nav-icon {
			color: #ffffff;
		}

		.nav-count {
			background: rgba(255, 255, 255, 0.22);
			color: #ffffff;
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
	font-size: 0.6875rem;
	font-weight: 500;
	padding: 0.05rem 0.4rem;
	border-radius: 9999px;
	background: #f1f5f9;
	color: #64748b;
}

.nav-section-header {
	font-size: 0.625rem;
	font-weight: 700;
	letter-spacing: 0.08em;
	color: #94a3b8;
	padding: 0.625rem 0.65rem 0.25rem;
	text-transform: uppercase;
}

.category-list {
	display: flex;
	flex-direction: column;
	gap: 0.1rem;
}

.sidebar-footer {
	padding-top: 0.75rem;
	border-top: 1px solid #f1f5f9;
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
}

.footer-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	gap: 0.45rem;
	width: 100%;
	height: 32px;
	padding: 0 0.65rem;
	border-radius: 0.375rem;
	font-size: 0.78125rem;
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;
	line-height: 1;

	.footer-icon {
		font-size: 15px;
		line-height: 1;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		vertical-align: middle;
	}

	&.custom-install-btn {
		background: #eff6ff;
		color: #2563eb;
		border: 1px solid #bfdbfe;

		.footer-icon {
			color: #2563eb;
		}

		&:hover, &.is-active {
			background: #2563eb;
			color: #ffffff;
			border-color: #2563eb;

			.footer-icon {
				color: #ffffff;
			}
		}
	}

	&.sources-btn {
		background: #f8fafc;
		color: #64748b;
		border: 1px solid #e2e8f0;

		.footer-icon {
			color: #64748b;
		}

		&:hover {
			background: #f1f5f9;
			color: #334155;
			border-color: #cbd5e1;
		}
	}
}

/* Main Content Area */
.appstore-main {
	flex: 1;
	display: flex;
	flex-direction: column;
	min-width: 0;
	background: #f8fafc;
}

.main-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 1.5rem;
	background: #ffffff;
	border-bottom: 1px solid #e2e8f0;
	gap: 1rem;
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
	color: #94a3b8;
	font-size: 16px;
	pointer-events: none;
}

.search-input {
	width: 100%;
	padding: 0.5rem 2.25rem 0.5rem 2.35rem;
	border-radius: 0.375rem;
	border: 1px solid #cbd5e1;
	background: #f8fafc;
	font-size: 0.8125rem;
	color: #0f172a;
	outline: none;
	transition: all 0.15s ease;

	&:focus {
		border-color: #2563eb;
		background: #ffffff;
		box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.1);
	}
}

.clear-search-btn {
	position: absolute;
	right: 0.75rem;
	border: none;
	background: transparent;
	color: #94a3b8;
	font-size: 14px;
	cursor: pointer;

	&:hover {
		color: #475569;
	}
}

.header-actions {
	display: flex;
	align-items: center;
	gap: 0.5rem;
}

.filter-btn {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	padding: 0.4rem 0.75rem;
	border-radius: 0.375rem;
	border: 1px solid #cbd5e1;
	background: #ffffff;
	color: #334155;
	font-size: 0.78125rem;
	font-weight: 500;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: 14px;
	}

	&:hover {
		background: #f1f5f9;
	}
}

.icon-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 32px;
	height: 32px;
	border-radius: 0.375rem;
	border: 1px solid #cbd5e1;
	background: #ffffff;
	color: #475569;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: 16px;
	}

	&:hover {
		background: #f1f5f9;
		color: #0f172a;
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
	padding: 1.25rem 1.5rem 2.5rem;
}

/* Discover Section & Hero Banner */
.hero-carousel-wrapper {
	margin-bottom: 2rem;
}

.hero-carousel {
	position: relative;
	height: 240px;
	border-radius: 0.875rem;
	overflow: hidden;
	background: #0f172a;
	box-shadow: 0 8px 20px -4px rgba(0, 0, 0, 0.12);
}

.hero-slide {
	position: absolute;
	inset: 0;
	opacity: 0;
	transition: opacity 0.35s ease;
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 1.75rem 2.5rem;
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
	gap: 0.3rem;
	padding: 0.15rem 0.5rem;
	border-radius: 9999px;
	background: rgba(234, 179, 8, 0.18);
	border: 1px solid rgba(234, 179, 8, 0.35);
	color: #fde047;
	font-size: 0.65625rem;
	font-weight: 700;
	letter-spacing: 0.05em;
	margin-bottom: 0.625rem;

	i.mdi {
		font-size: 12px;
	}
}

.hero-app-info {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	margin-bottom: 0.5rem;
}

.hero-app-icon {
	width: 44px;
	height: 44px;
	border-radius: 0.625rem;
	box-shadow: 0 3px 10px rgba(0, 0, 0, 0.3);
	background: #ffffff;
	padding: 2px;
	flex-shrink: 0;
}

.hero-text-col {
	display: flex;
	flex-direction: column;
}

.hero-app-title {
	font-size: 1.25rem;
	font-weight: 700;
	color: #ffffff;
	line-height: 1.2;
}

.hero-app-meta {
	font-size: 0.71875rem;
	color: #94a3b8;
	font-weight: 400;
}

.hero-app-tagline {
	font-size: 0.8125rem;
	color: #cbd5e1;
	text-shadow: none !important;
	display: -webkit-box;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
	overflow: hidden;
	line-height: 1.4;
	margin-bottom: 1rem;
}

.hero-actions {
	display: flex;
	align-items: center;
	gap: 0.5rem;
}

.hero-action-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	padding: 0.45rem 1rem;
	border-radius: 9999px;
	font-size: 0.78125rem;
	font-weight: 600;
	border: none;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: 14px;
	}

	&.is-install {
		background: #2563eb;
		color: #ffffff;

		&:hover {
			background: #1d4ed8;
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
	gap: 0.2rem;
	padding: 0.45rem 0.65rem;
	border-radius: 9999px;
	background: transparent;
	color: #cbd5e1;
	font-size: 0.78125rem;
	font-weight: 500;
	border: none;
	cursor: pointer;

	i.mdi {
		font-size: 13px;
	}

	&:hover {
		color: #ffffff;
	}
}

.hero-preview-box {
	position: relative;
	z-index: 2;
	width: 250px;
	height: 145px;
	border-radius: 0.75rem;
	overflow: hidden;
	box-shadow: 0 12px 24px rgba(0, 0, 0, 0.4);
	border: 1px solid rgba(255, 255, 255, 0.12);
	cursor: pointer;
	transition: transform 0.2s ease;
	background: #0f172a;

	&:hover {
		transform: scale(1.02);
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
		font-size: 16px;
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
	gap: 0.35rem;
}

.carousel-dot {
	width: 6px;
	height: 6px;
	border-radius: 9999px;
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
	margin-bottom: 2rem;
}

.section-header {
	display: flex;
	align-items: flex-end;
	justify-content: space-between;
	margin-bottom: 0.875rem;
}

.section-title {
	font-size: 1.0625rem;
	font-weight: 700;
	color: #0f172a;
	margin-bottom: 0.125rem;
}

.section-subtitle {
	font-size: 0.78125rem;
	font-weight: 400;
	color: #64748b;
}

.see-all-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.2rem;
	background: transparent;
	border: none;
	color: #2563eb;
	font-size: 0.78125rem;
	font-weight: 600;
	cursor: pointer;

	i.mdi {
		font-size: 13px;
	}

	&:hover {
		color: #1d4ed8;
	}
}

.catalog-header {
	margin-bottom: 0.875rem;
}

.catalog-title {
	font-size: 1.1875rem;
	font-weight: 700;
	color: #0f172a;
	margin-bottom: 0.125rem;
}

.catalog-subtitle {
	font-size: 0.78125rem;
	font-weight: 400;
	color: #64748b;
}

/* App Cards Grid */
.app-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
	gap: 1.125rem;
}

.app-card {
	background: #ffffff;
	border: 1px solid #e2e8f0;
	border-radius: 0.75rem;
	overflow: hidden;
	display: flex;
	flex-direction: column;
	cursor: pointer;
	transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
	text-shadow: none !important;

	* {
		text-shadow: none !important;
	}

	&:hover {
		transform: translateY(-2px);
		box-shadow: 0 8px 18px -3px rgba(0, 0, 0, 0.08);
		border-color: #cbd5e1;

		.card-banner-img {
			transform: scale(1.02);
		}
	}
}

.card-banner {
	position: relative;
	width: 100%;
	height: 145px;
	overflow: hidden;
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
	font-size: 28px;
}

.app-card-body {
	padding: 0.875rem;
	display: flex;
	flex-direction: column;
	flex: 1;
}

.app-card-top {
	display: flex;
	gap: 0.65rem;
	margin-bottom: 0.45rem;
}

.app-icon {
	width: 40px;
	height: 40px;
	border-radius: 0.5rem;
	object-fit: cover;
	box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
	flex-shrink: 0;
	background: #f8fafc;
	padding: 2px;
	border: 1px solid #f1f5f9;
}

.app-info {
	flex: 1;
	min-width: 0;
}

.app-title {
	font-size: 0.875rem;
	font-weight: 600;
	color: #0f172a;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	line-height: 1.3;
}

.app-author {
	font-size: 0.71875rem;
	font-weight: 400;
	color: #94a3b8;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	display: block;
}

.app-tagline {
	font-size: 0.78125rem;
	font-weight: 400;
	color: #64748b;
	line-height: 1.4;
	text-shadow: none !important;
	display: -webkit-box;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
	overflow: hidden;
	margin-bottom: 0.65rem;
	flex: 1;
}

.app-card-bottom {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding-top: 0.55rem;
	border-top: 1px solid #f1f5f9;
	gap: 0.4rem;
}

.app-meta-group {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	min-width: 0;
	flex: 1;
	overflow: hidden;
}

.app-cat-pill {
	font-size: 0.65625rem;
	font-weight: 500;
	color: #475569;
	background: #f1f5f9;
	padding: 0.125rem 0.45rem;
	border-radius: 9999px;
	white-space: nowrap;
}

.app-arch-text {
	font-size: 0.65625rem;
	color: #94a3b8;
	font-weight: 400;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.card-btn {
	padding: 0.3rem 0.875rem;
	border-radius: 9999px;
	font-size: 0.78125rem;
	font-weight: 600;
	border: none;
	cursor: pointer;
	transition: all 0.15s ease;
	white-space: nowrap;

	&.is-install {
		background: #2563eb;
		color: #ffffff;

		&:hover {
			background: #1d4ed8;
		}

		&:disabled {
			background: #93c5fd;
			color: #ffffff;
			cursor: not-allowed;
		}
	}

	&.is-open {
		background: #eff6ff;
		color: #2563eb;
		border: 1px solid #bfdbfe;

		&:hover {
			background: #dbeafe;
		}
	}
}

.card-btn-split {
	display: inline-flex;
	align-items: center;
	border-radius: 9999px;
	overflow: hidden;
	background: #2563eb;
	box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);

	.card-btn.is-install {
		border-radius: 0;
		padding: 0.3rem 0.7rem;
	}

	.card-btn-cog {
		background: #1d4ed8;
		color: #ffffff;
		border: none;
		padding: 0.3rem 0.45rem;
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: background 0.15s ease;

		i.mdi {
			font-size: 13px;
		}

		&:hover {
			background: #1e40af;
		}

		&:disabled {
			background: #cbd5e1;
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
	background: #f1f5f9;
	overflow: hidden;
}

.installer-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 1.5rem;
	border-bottom: 1px solid #e2e8f0;
	background: #ffffff;
	gap: 1rem;
}

.installer-header-left {
	display: flex;
	align-items: center;
	gap: 1rem;
}

.back-to-store-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	padding: 0.4rem 0.75rem;
	border-radius: 0.375rem;
	border: 1px solid #e2e8f0;
	background: #f8fafc;
	color: #475569;
	font-size: 0.78125rem;
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: 15px;
	}

	&:hover {
		background: #f1f5f9;
		color: #0f172a;
	}
}

.installer-app-badge {
	display: flex;
	align-items: center;
	gap: 0.65rem;
}

.installer-app-icon {
	width: 32px;
	height: 32px;
	border-radius: 0.375rem;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	padding: 2px;
	object-fit: cover;
}

.installer-app-info {
	display: flex;
	flex-direction: column;
}

.installer-app-title {
	font-size: 0.9375rem;
	font-weight: 700;
	color: #0f172a;
	line-height: 1.2;
}

.installer-app-sub {
	font-size: 0.6875rem;
	color: #64748b;
	font-family: monospace;
}

.view-switch-pills {
	display: inline-flex;
	align-items: center;
	background: #f1f5f9;
	padding: 3px;
	border-radius: 0.5rem;
	border: 1px solid #e2e8f0;
}

.view-pill-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	padding: 0.35rem 0.875rem;
	border-radius: 0.375rem;
	border: none;
	background: transparent;
	color: #64748b;
	font-size: 0.78125rem;
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: 15px;
	}

	&.is-active {
		background: #ffffff;
		color: #2563eb;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
	}
}

.installer-header-right {
	display: flex;
	align-items: center;
	gap: 0.5rem;
}

.tool-action-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	padding: 0.4rem 0.75rem;
	border-radius: 0.375rem;
	border: 1px solid #cbd5e1;
	background: #ffffff;
	color: #475569;
	font-size: 0.78125rem;
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: 15px;
	}

	&:hover {
		background: #f1f5f9;
		color: #0f172a;
	}
}

.installer-body {
	flex: 1;
	overflow-y: auto;
	padding: 1.25rem 1.75rem 2.5rem;
}

/* Card-Based Visual Editor */
.installer-form-layout {
	max-width: 960px;
	margin: 0 auto;
	display: flex;
	flex-direction: column;
	gap: 1.25rem;
}

.installer-card {
	background: #ffffff;
	border-radius: 0.75rem;
	border: 1px solid #e2e8f0;
	padding: 1.25rem 1.5rem;
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.03);
}

.card-title-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	margin-bottom: 0.5rem;

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
	border-radius: 0.5rem;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 18px;
	flex-shrink: 0;

	&.is-blue { background: #eff6ff; color: #2563eb; }
	&.is-emerald { background: #ecfdf5; color: #059669; }
	&.is-indigo { background: #eef2ff; color: #4f46e5; }
	&.is-amber { background: #fffbeb; color: #d97706; }
	&.is-purple { background: #faf5ff; color: #9333ea; }
	&.is-slate { background: #f1f5f9; color: #475569; }
}

.card-heading {
	font-size: 0.9375rem;
	font-weight: 700;
	color: #0f172a;
	line-height: 1.2;
}

.card-caption {
	font-size: 0.75rem;
	color: #64748b;
}

.card-toggle-wrap {
	margin-left: auto;
}

.accordion-toggle-btn {
	background: transparent;
	border: none;
	color: #64748b;
	font-size: 20px;
	cursor: pointer;
}

/* Forms & Inputs */
.form-grid-2 {
	display: grid;
	grid-template-columns: repeat(2, 1fr);
	gap: 1rem;
}

.form-grid-3 {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 1rem;
}

.form-group {
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
}

.form-label {
	font-size: 0.78125rem;
	font-weight: 600;
	color: #334155;

	.req {
		color: #ef4444;
	}
}

.form-input, .form-select {
	width: 100%;
	padding: 0.5rem 0.75rem;
	border-radius: 0.375rem;
	border: 1px solid #cbd5e1;
	background: #f8fafc;
	font-size: 0.8125rem;
	color: #0f172a;
	outline: none;
	transition: all 0.15s ease;

	&:focus {
		border-color: #2563eb;
		background: #ffffff;
		box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.1);
	}

	&.is-sm {
		padding: 0.4rem 0.6rem;
		font-size: 0.78125rem;
	}

	&.is-mono {
		font-family: monospace;
	}
}

.quick-tags {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	margin-top: 0.25rem;
}

.quick-tag-label {
	font-size: 0.6875rem;
	color: #94a3b8;
}

.quick-tag-btn {
	background: #f1f5f9;
	border: 1px solid #e2e8f0;
	color: #475569;
	border-radius: 0.25rem;
	padding: 0.1rem 0.35rem;
	font-size: 0.6875rem;
	font-family: monospace;
	cursor: pointer;

	&:hover {
		background: #e2e8f0;
		color: #0f172a;
	}
}

.icon-input-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
}

.icon-preview-thumb {
	width: 38px;
	height: 38px;
	border-radius: 0.5rem;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	padding: 2px;
	object-fit: cover;
	flex-shrink: 0;
}

/* Dynamic Rows */
.dynamic-list {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
}

.dynamic-row {
	display: flex;
	align-items: flex-end;
	gap: 0.5rem;
	padding: 0.5rem 0.75rem;
	border-radius: 0.5rem;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
}

.dynamic-field {
	flex: 1;
	display: flex;
	flex-direction: column;
	gap: 0.2rem;

	&.is-wide {
		flex: 2;
	}

	&.is-protocol {
		flex: 0 0 90px;
	}

	&.is-mode {
		flex: 0 0 160px;
	}
}

.field-mini-label {
	font-size: 0.65625rem;
	font-weight: 600;
	text-transform: uppercase;
	color: #94a3b8;
}

.row-arrow {
	color: #94a3b8;
	font-size: 14px;
	margin-bottom: 0.5rem;
}

.delete-row-btn {
	background: transparent;
	border: none;
	color: #94a3b8;
	font-size: 18px;
	cursor: pointer;
	padding: 0.35rem;
	margin-bottom: 0.2rem;
	border-radius: 0.25rem;
	display: flex;
	align-items: center;
	justify-content: center;

	&:hover {
		background: #fee2e2;
		color: #ef4444;
	}
}

.add-row-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: 0.35rem;
	width: 100%;
	padding: 0.5rem;
	border-radius: 0.5rem;
	border: 1px dashed #cbd5e1;
	background: #ffffff;
	color: #2563eb;
	font-size: 0.78125rem;
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	i.mdi {
		font-size: 15px;
	}

	&:hover {
		background: #eff6ff;
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
	border-radius: 0.75rem;
	overflow: hidden;
	border: 1px solid #1e293b;
	box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.yaml-studio-bar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.5rem 1rem;
	background: #090d16;
	border-bottom: 1px solid #1e293b;
}

.yaml-stat {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	font-size: 0.75rem;
	font-family: monospace;
	color: #94a3b8;
}

.copy-yaml-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.3rem;
	padding: 0.25rem 0.55rem;
	border-radius: 0.25rem;
	border: 1px solid #334155;
	background: #1e293b;
	color: #cbd5e1;
	font-size: 0.6875rem;
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
	padding: 1.25rem;
	background: #0f172a;
	color: #38bdf8;
	font-family: 'JetBrains Mono', 'Fira Code', Consolas, Monaco, monospace;
	font-size: 0.8125rem;
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
	padding: 0.875rem 1.75rem;
	border-top: 1px solid #e2e8f0;
	background: #ffffff;
	gap: 1rem;
}

.installer-footer-info {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	font-size: 0.75rem;
	color: #64748b;

	i.mdi {
		color: #2563eb;
		font-size: 15px;
	}
}

.installer-footer-btns {
	display: flex;
	align-items: center;
	gap: 0.75rem;
}

.installer-cancel-btn {
	padding: 0.45rem 1.125rem;
	border-radius: 0.375rem;
	background: transparent;
	border: 1px solid #cbd5e1;
	color: #475569;
	font-size: 0.8125rem;
	font-weight: 600;
	cursor: pointer;

	&:hover {
		background: #f1f5f9;
		color: #0f172a;
	}
}

.installer-deploy-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.45rem;
	padding: 0.45rem 1.5rem;
	border-radius: 0.375rem;
	background: #2563eb;
	border: none;
	color: #ffffff;
	font-size: 0.8125rem;
	font-weight: 600;
	cursor: pointer;
	transition: background 0.15s ease;

	i.mdi {
		font-size: 16px;
	}

	&:hover {
		background: #1d4ed8;
	}

	&:disabled {
		background: #cbd5e1;
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
	background: #e2e8f0;
	animation: pulse 1.5s infinite;
}

.skeleton-icon {
	width: 40px;
	height: 40px;
	border-radius: 0.5rem;
	background: #e2e8f0;
	animation: pulse 1.5s infinite;
}

.skeleton-info {
	flex: 1;
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
}

.skeleton-line {
	height: 11px;
	background: #e2e8f0;
	border-radius: 3px;
	animation: pulse 1.5s infinite;

	&.is-title {
		width: 70%;
	}
	&.is-subtitle {
		width: 90%;
	}
	&.is-tag {
		width: 45px;
		margin-top: 0.4rem;
	}
}

@keyframes pulse {
	0%, 100% { opacity: 1; }
	50% { opacity: 0.5; }
}

/* Empty State */
.empty-state {
	text-align: center;
	padding: 3.5rem 2rem;
	color: #64748b;
}

.empty-icon {
	color: #cbd5e1;
	font-size: 40px;
	margin-bottom: 0.75rem;
}

.empty-title {
	font-size: 1.0625rem;
	font-weight: 600;
	color: #334155;
	margin-bottom: 0.4rem;
}

.empty-desc {
	font-size: 0.8125rem;
	margin-bottom: 1.25rem;
}

.empty-action-btn {
	padding: 0.45rem 1.125rem;
	border-radius: 0.375rem;
	background: #2563eb;
	color: #ffffff;
	border: none;
	font-weight: 600;
	cursor: pointer;
}

/* App Detail Drawer */
.app-detail-drawer {
	position: absolute;
	inset: 0;
	background: rgba(15, 23, 42, 0.4);
	backdrop-filter: blur(4px);
	z-index: 100;
	display: flex;
	justify-content: flex-end;
}

.drawer-panel {
	width: 640px;
	max-width: 90%;
	height: 100%;
	background: #ffffff;
	box-shadow: -10px 0 30px rgba(0, 0, 0, 0.12);
	display: flex;
	flex-direction: column;
}

.drawer-header {
	display: flex;
	align-items: center;
	padding: 0.875rem 1.25rem;
	border-bottom: 1px solid #e2e8f0;
}

.back-btn {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	background: transparent;
	border: none;
	color: #475569;
	font-size: 0.8125rem;
	font-weight: 600;
	cursor: pointer;

	i.mdi {
		font-size: 15px;
	}

	&:hover {
		color: #0f172a;
	}
}

.drawer-header-spacer {
	flex: 1;
}

.drawer-close-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 28px;
	height: 28px;
	border-radius: 50%;
	border: none;
	background: #f1f5f9;
	color: #64748b;
	cursor: pointer;

	i.mdi {
		font-size: 16px;
	}

	&:hover {
		background: #e2e8f0;
		color: #0f172a;
	}
}

.drawer-content {
	flex: 1;
	overflow-y: auto;
	padding: 1.5rem 1.75rem 3rem;
}

.detail-hero {
	display: flex;
	gap: 1.25rem;
	margin-bottom: 1.75rem;
	padding-bottom: 1.25rem;
	border-bottom: 1px solid #f1f5f9;
}

.detail-icon {
	width: 64px;
	height: 64px;
	border-radius: 0.875rem;
	box-shadow: 0 4px 10px -2px rgba(0, 0, 0, 0.08);
	flex-shrink: 0;
	border: 1px solid #f1f5f9;
}

.detail-hero-info {
	flex: 1;
}

.detail-title {
	font-size: 1.25rem;
	font-weight: 700;
	color: #0f172a;
	margin-bottom: 0.2rem;
}

.detail-tagline {
	font-size: 0.8125rem;
	color: #64748b;
	margin-bottom: 0.65rem;
	line-height: 1.4;
}

.detail-meta-row {
	display: flex;
	gap: 0.4rem;
	margin-bottom: 1rem;
	flex-wrap: wrap;
}

.detail-pill {
	font-size: 0.71875rem;
	font-weight: 500;
	padding: 0.15rem 0.55rem;
	border-radius: 9999px;
	background: #eff6ff;
	color: #2563eb;

	&.is-subtle {
		background: #f1f5f9;
		color: #64748b;
	}

	&.is-danger {
		background: #fef2f2;
		color: #dc2626;
	}
}

.detail-actions {
	display: flex;
	gap: 0.625rem;
}

.detail-action-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.4rem;
	padding: 0.45rem 1.25rem;
	border-radius: 9999px;
	font-size: 0.8125rem;
	font-weight: 600;
	border: none;
	cursor: pointer;

	i.mdi {
		font-size: 14px;
	}

	&.is-install {
		background: #2563eb;
		color: #ffffff;

		&:hover {
			background: #1d4ed8;
		}

		&.is-loading {
			background: #3b82f6;
		}
	}

	&.is-customize {
		background: #eff6ff;
		color: #2563eb;
		border: 1px solid #bfdbfe;

		&:hover {
			background: #dbeafe;
		}
	}

	&.is-open {
		background: #eff6ff;
		color: #2563eb;
		border: 1px solid #bfdbfe;

		&:hover {
			background: #dbeafe;
		}
	}
}

.detail-section {
	margin-bottom: 1.75rem;
}

.detail-section-title {
	font-size: 0.9375rem;
	font-weight: 700;
	color: #0f172a;
	margin-bottom: 0.75rem;
}

.screenshots-gallery {
	display: flex;
	gap: 0.875rem;
	overflow-x: auto;
	padding-bottom: 0.4rem;

	&::-webkit-scrollbar {
		height: 5px;
	}
	&::-webkit-scrollbar-thumb {
		background: #cbd5e1;
		border-radius: 3px;
	}
}

.screenshot-item {
	position: relative;
	flex-shrink: 0;
	width: 250px;
	height: 145px;
	border-radius: 0.625rem;
	overflow: hidden;
	border: 1px solid #e2e8f0;
	cursor: pointer;
	box-shadow: 0 2px 4px rgba(0, 0, 0, 0.04);
	transition: transform 0.15s ease;

	&:hover {
		transform: scale(1.02);

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
	font-size: 20px;
	opacity: 0;
	transition: opacity 0.2s ease;
}

.detail-description {
	background: #f8fafc;
	border-radius: 0.625rem;
	padding: 1rem;
	border: 1px solid #e2e8f0;
}

.description-text {
	font-size: 0.8125rem;
	line-height: 1.55;
	color: #334155;
	white-space: pre-line;
	text-shadow: none !important;
}

.specs-grid {
	display: grid;
	grid-template-columns: repeat(2, 1fr);
	gap: 0.75rem;
}

.spec-card {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 0.5rem;
	padding: 0.65rem 0.75rem;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.spec-label {
	font-size: 0.65625rem;
	font-weight: 600;
	text-transform: uppercase;
	color: #94a3b8;
}

.spec-value {
	font-size: 0.78125rem;
	font-weight: 600;
	color: #1e293b;
}

/* Lightbox */
.lightbox-overlay {
	position: fixed;
	inset: 0;
	background: rgba(0, 0, 0, 0.85);
	backdrop-filter: blur(8px);
	z-index: 200;
	display: flex;
	align-items: center;
	justify-content: center;
	padding: 2rem;
	cursor: pointer;
}

.lightbox-img {
	max-width: 90vw;
	max-height: 90vh;
	border-radius: 0.625rem;
	box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
	object-fit: contain;
}

.lightbox-close-btn {
	position: absolute;
	top: 1.5rem;
	right: 1.5rem;
	width: 38px;
	height: 38px;
	border-radius: 50%;
	border: none;
	background: rgba(255, 255, 255, 0.2);
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 20px;
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.35);
	}
}

/* Sources Modal */
.sources-overlay {
	position: absolute;
	inset: 0;
	background: rgba(15, 23, 42, 0.45);
	backdrop-filter: blur(4px);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 110;
}

.sources-panel {
	width: 540px;
	max-width: 90%;
	background: #ffffff;
	border-radius: 0.75rem;
	box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.18);
	display: flex;
	flex-direction: column;
	overflow: hidden;
}

.sources-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 1rem 1.25rem;
	border-bottom: 1px solid #e2e8f0;
}

.sources-title {
	font-size: 1rem;
	font-weight: 700;
	color: #0f172a;
}

.sources-body {
	padding: 1.25rem;
}

.sources-footer {
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: 0.625rem;
	padding: 0.75rem 1.25rem;
	border-top: 1px solid #e2e8f0;
	background: #f8fafc;
}

.footer-done-btn {
	padding: 0.4rem 1rem;
	border-radius: 0.375rem;
	background: #2563eb;
	border: none;
	color: #ffffff;
	font-weight: 600;
	cursor: pointer;
}

.add-source-box {
	display: flex;
	gap: 0.5rem;
	margin-bottom: 1rem;
}

.source-input {
	flex: 1;
	padding: 0.45rem 0.65rem;
	border-radius: 0.375rem;
	border: 1px solid #cbd5e1;
	font-size: 0.78125rem;
	outline: none;

	&:focus {
		border-color: #2563eb;
	}
}

.add-source-btn {
	padding: 0.45rem 0.75rem;
	border-radius: 0.375rem;
	background: #2563eb;
	color: #ffffff;
	border: none;
	font-weight: 600;
	font-size: 0.78125rem;
	cursor: pointer;
	white-space: nowrap;

	&:disabled {
		background: #cbd5e1;
		cursor: not-allowed;
	}
}

.sources-list {
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
	max-height: 160px;
	overflow-y: auto;
}

.source-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.45rem 0.65rem;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 0.375rem;
}

.source-name {
	font-size: 0.78125rem;
	font-weight: 500;
	color: #334155;
	word-break: break-all;
}

.delete-source-btn {
	border: none;
	background: transparent;
	color: #ef4444;
	cursor: pointer;
	padding: 0.2rem;
	font-size: 15px;

	&:hover {
		color: #dc2626;
	}
}

/* Responsive Adaptations */
.appstore-app.is-compact {
	.appstore-sidebar {
		width: 56px;
		min-width: 56px;
		padding: 0.75rem 0.35rem;

		.brand-info, .nav-label, .nav-count, .nav-section-header, .footer-btn span {
			display: none;
		}

		.nav-item, .footer-btn {
			justify-content: center;
			padding: 0.45rem;
		}
	}

	.main-header {
		padding: 0.65rem 0.875rem;
	}

	.main-body {
		padding: 0.875rem 0.875rem 1.75rem;
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
.drawer-slide-enter-active, .drawer-slide-leave-active {
	transition: all 0.25s ease;
	.drawer-panel {
		transition: transform 0.25s ease;
	}
}

.drawer-slide-enter, .drawer-slide-leave-to {
	opacity: 0;
	.drawer-panel {
		transform: translateX(100%);
	}
}

.fade-enter-active, .fade-leave-active {
	transition: opacity 0.2s ease;
}

.fade-enter, .fade-leave-to {
	opacity: 0;
}
</style>
