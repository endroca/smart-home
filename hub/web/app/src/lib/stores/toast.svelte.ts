// Toast global (substitui o #toast do front antigo).

class ToastStore {
  message = $state("");
  error = $state(false);
  visible = $state(false);
  #timer: ReturnType<typeof setTimeout> | null = null;

  show(message: string, isError = false) {
    this.message = message;
    this.error = isError;
    this.visible = true;
    if (this.#timer) clearTimeout(this.#timer);
    this.#timer = setTimeout(() => {
      this.visible = false;
      this.#timer = null;
    }, 4500);
  }

  hide() {
    if (this.#timer) clearTimeout(this.#timer);
    this.#timer = null;
    this.visible = false;
  }
}

export const toast = new ToastStore();
