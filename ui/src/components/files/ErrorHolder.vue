<template>
    <div class="is-flex is-align-items-center is-justify-content-center is-flex-direction-column">
        <b-image :src="require('@/assets/img/sad.svg')" alt="pending" class="is-160x160 mb-2" />
        <div>{{ $t(error) }}</div>
    </div>
</template>

<script>
export default {
    name: "ErrorHolder",
    props: {
        error: {
            type: String,
            default: ""
        }
    }
}
</script>

<style lang="scss" scoped>

</style>