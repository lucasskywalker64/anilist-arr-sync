<script lang="ts">
  export let className = '';
</script>

<div class="tableContainer">
  <table class="table {className}">
    <slot />
  </table>
</div>

<style>
  .tableContainer {
    width: 100%;
    overflow-x: auto;
  }

  .table {
    width: 100%;
    max-width: 100%;
    border-collapse: collapse;
    font-size: var(--defaultFontSize);
  }

  :global(.table th) {
    padding: 10px 8px;
    border: none;
    text-align: left;
    font-weight: bold;
    color: var(--textColor);
    border-bottom: 2px solid var(--borderColor);
  }

  :global(.table tr) {
    transition: background-color 500ms ease;
  }

  :global(.table tbody tr:hover) {
    background-color: var(--tableRowHoverBackgroundColor);
  }

  :global(.table td) {
    padding: 10px 8px;
    border-top: 1px solid var(--borderColor);
    line-height: var(--lineHeight);
    vertical-align: middle;
  }
</style>
