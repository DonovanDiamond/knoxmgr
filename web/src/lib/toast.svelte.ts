class Toast {
	message = $state('');
	private timer: ReturnType<typeof setTimeout> | undefined;

	show(message: string) {
		this.message = message;
		clearTimeout(this.timer);
		this.timer = setTimeout(() => (this.message = ''), 3500);
	}
}

export const toast = new Toast();
