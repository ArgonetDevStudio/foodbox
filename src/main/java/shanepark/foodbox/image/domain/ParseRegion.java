package shanepark.foodbox.image.domain;

public record ParseRegion(int x, int y, int width, int height) {

    public ParseRegion addX(int i) {
        return new ParseRegion(this.x + i, y, width, height);
    }

    public boolean contains(int x, int y) {
        return this.x <= x && x <= this.x + width && this.y <= y && y <= this.y + height;
    }

}
