<footer class="footer">
  <slot />
</footer>

<style>
  .footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex: 0 0 auto;
    padding: 15px 20px;
    background-color: var(--pageFooterBackground);
    border-top: 1px solid rgba(255, 255, 255, 0.06);
    box-shadow: 0 -2px 10px rgba(0, 0, 0, 0.3);
    z-index: 2;
  }
</style>
