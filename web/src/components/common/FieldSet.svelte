<script lang="ts">
  export let legend = '';
</script>

<fieldset class="fieldSet">
  {#if legend}
    <legend class="legend">{legend}</legend>
  {/if}
  <slot />
</fieldset>

<style>
  .fieldSet {
    margin: 0 0 25px 0;
    padding: 0;
    border: 0;
  }

  .legend {
    display: block;
    width: 100%;
    margin-bottom: 21px;
    padding: 0 0 6px 0;
    border: 0;
    border-bottom: 1px solid rgba(229, 229, 229, 0.2);
    color: var(--textColor);
    font-size: 21px;
    font-weight: 300;
  }
</style>
