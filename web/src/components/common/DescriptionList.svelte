<dl class="descriptionList">
  <slot />
</dl>

<style>
  .descriptionList {
    margin: 0;
    padding: 0;
  }

  :global(.descriptionList dt) {
    font-weight: bold;
    float: left;
    clear: left;
    width: 160px;
    text-align: right;
    line-height: var(--lineHeight);
    color: var(--defaultColor);
  }

  :global(.descriptionList dd) {
    margin-left: 180px;
    line-height: var(--lineHeight);
    overflow-wrap: break-word;
    margin-bottom: 8px;
    color: var(--textColor);
  }

  @media (max-width: 600px) {
    :global(.descriptionList dt) {
      float: none;
      width: auto;
      text-align: left;
    }
    :global(.descriptionList dd) {
      margin-left: 0;
    }
  }
</style>
