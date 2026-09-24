<template>
  <div class="modal-card">
    <!-- Modal-Card Header Start -->
    <header class="modal-card-head">
      <div class="is-flex-grow-1">
        <h3 class="title is-header">{{ $t('Import') }}</h3>
      </div>
    </header>
    <!-- Modal-Card Header End -->
    <!-- Modal-Card Body Start -->
    <section class="modal-card-body">
      <b-tabs v-model="activeTab" :animated="false" @input="errors = ''">
        <b-tab-item label="Docker Compose">
          <b-field :message="errors" :type="{ 'is-danger': !!errors }">
            <b-input v-model="dockerComposeCommands" :aria-label="$t('Docker Compose YAML')" :placeholder="$t('Notice: If there are multiple services, only the first set can be analyzed correctly')" class="import-area" type="textarea"></b-input>
          </b-field>

          <b-upload ref="importUpload" v-model="dropFiles" accept=".yaml,.yml" drag-drop expanded @input="onSelect">
            <section class="section">
              <div class="content has-text-centered">
                <p>
                  <b-icon :icon="uploadIcon" custom-size="is-size-2" size="is-40"></b-icon>
                </p>
                <p class="has-text-full-03">{{ dropText }}</p>
              </div>
            </section>
          </b-upload>

        </b-tab-item>
        <b-tab-item label="Docker CLI">
          <b-field :message="errors" :type="{ 'is-danger': !!errors }" class="mb-0">
            <b-input v-model="dockerCliCommands" :aria-label="$t('Docker run command')" placeholder="docker run -d -p 8080:80 nginx" class="import-area-cli" type="textarea"></b-input>
          </b-field>
        </b-tab-item>
      </b-tabs>
    </section>
    <!-- Modal-Card Body End -->
    <!-- Modal-Card Footer Start-->
    <footer class="modal-card-foot is-flex is-align-items-center">
      <div class="is-flex-grow-1 has-text-full-04">
        
      </div>
      <div>
        <b-button :label="$t('Cancel')" rounded @click="$emit('close')" />
        <b-button :label="$t('Submit')" rounded type="is-primary" @click="emitSubmit" />
      </div>
    </footer>
    <!-- Modal-Card Footer End -->
  </div>
</template>

<script>

import { parse, stringify } from "yaml"
import composerize from "composerize";

export default {
  data() {
    return {
      activeTab: 0,
      dropFiles: null,
      dockerCliCommands: "",
      dockerComposeCommands: "",
      errors: "",
      dropText: this.$t('Drop your Docker Compose file here or click to upload'),
      uploadIcon: "upload",
    }
  },
  props: {
    netWorks: Array,
    oriNetWorks: Array,
    deviceMemory: Number,
    // Called directly when opened as a standalone desktop window - the
    // shared window chrome only forwards close/minimize, not custom
    // business events like 'update'.
    onUpdate: {
      type: Function,
      default: null
    }
  },
  methods: {
    emitSubmit() {
      this.errors = ""
      let yaml = ""
      if (this.activeTab == 1) {
        const cleanedCommand = this.dockerCliCommands.replace(/`#.*?`/g, '').replace(/#.*$/gm, '').trim();
        if (!cleanedCommand) {
          this.errors = this.$t('Paste a docker run command first.')
          return
        }
        try {
          yaml = composerize(cleanedCommand)
        } catch (e) {
          this.errors = this.$t('Could not convert this command: {error}', { error: (e && e.message) || String(e) })
          return
        }
      } else {
        yaml = this.dockerComposeCommands
      }
      const result = this.normalizeCompose(yaml)
      if (result.error) {
        this.errors = result.error
        return
      }
      this.dockerComposeCommands = result.yaml
      this.$emit('update', result.yaml)
      if (typeof this.onUpdate === 'function') this.onUpdate(result.yaml)
      this.$emit('close')
    },

    // Validates a compose document and fills in the NivaroOS (x-casaos)
    // app metadata it needs, keeping whatever the file already has
    // (port_map, tips, icon, index...). Returns { yaml } or { error }.
    normalizeCompose(text) {
      if (!text || !String(text).trim()) {
        return { error: this.$t('Paste a Docker Compose file or upload one first.') }
      }
      let doc
      try {
        doc = parse(text)
      } catch (e) {
        const where = e && e.linePos && e.linePos[0] ? ` (${this.$t('line {line}', { line: e.linePos[0].line })})` : ''
        return { error: this.$t('This is not valid YAML: {error}', { error: ((e && e.message) || String(e)).split('\n')[0] + where }) }
      }
      if (!doc || typeof doc !== 'object' || Array.isArray(doc)) {
        return { error: this.$t('This is not a Docker Compose file - it has no "services" section.') }
      }
      const services = doc.services
      if (!services || typeof services !== 'object' || Array.isArray(services) || !Object.keys(services).length) {
        return { error: this.$t('This is not a Docker Compose file - it has no "services" section.') }
      }
      const names = Object.keys(services)
      const bad = names.find(n => !services[n] || typeof services[n] !== 'object')
      if (bad) {
        return { error: this.$t('Service "{name}" is empty or invalid.', { name: bad }) }
      }
      if (!names.some(n => services[n].image || services[n].build)) {
        return { error: this.$t('No service has an "image" to run.') }
      }

      const existing = doc['x-casaos'] && typeof doc['x-casaos'] === 'object' ? doc['x-casaos'] : {}
      // The main service: x-casaos.main, then the top-level "name" if it
      // names a service, then the first service.
      const main = (existing.main && services[existing.main]) ? existing.main
        : (doc.name && services[doc.name] ? doc.name : names[0])
      const merged = { ...existing, main }
      if (!merged.title || typeof merged.title !== 'object' || !Object.keys(merged.title).length) {
        merged.title = { en_us: main }
      }
      // No icon invented here: the UI shows its own built-in default for
      // apps without one (the old CasaOS CDN guess was usually a 404).
      doc['x-casaos'] = merged
      return { yaml: stringify(doc) }
    },

    onSelect(file) {
      if (!file) return
      if (typeof FileReader === "undefined") {
        this.errors = this.$t('Your browser does not support file reading.')
        return;
      }
      const reader = new FileReader();
      reader.onload = () => {
        this.errors = ""
        this.dockerComposeCommands = String(reader.result || '')
        this.dropText = file.name || this.dropText
      }
      reader.onerror = () => {
        this.errors = this.$t('Could not read {name}.', { name: file.name || this.$t('the file') })
      }
      reader.readAsText(file)
    },
  },
}
</script>

<style lang="scss" scoped>
.import-area {
	::v-deep .textarea {
		height: 22rem;
	}
}

.import-area-cli {
	::v-deep .textarea {
		height: 30rem;
	}
}

.control {
	min-height: 7.25rem;

	.section {
		padding: var(--space-3);
	}
}
</style>