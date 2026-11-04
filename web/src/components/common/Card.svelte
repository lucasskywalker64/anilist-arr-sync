<script lang="ts">
  export let className = '';
</script>

<div class="card {className}">
  <slot />
</div>

<style>
  .card {
    position: relative;
    margin: 10px 0;
    padding: 15px 20px;
    border-radius: 3px;
    background-color: var(--cardBackgroundColor);
    box-shadow: 0 0 10px 1px var(--cardShadowColor);
    color: var(--defaultColor);
  }
</style>
