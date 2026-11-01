package com.demo.library.member;

import java.util.Objects;
import org.springframework.stereotype.Component;

@Component
public class PlanPolicy {
    private static final String PREMIUM = "PREMIUM";
    private static final int PREMIUM_LIMIT = 10;
    private static final int STANDARD_LIMIT = 3;

    /** How many books a member on this plan can hold at once. */
    public int limitFor(String plan) {
        return Objects.equals(plan, PREMIUM) ? PREMIUM_LIMIT : STANDARD_LIMIT;
    }

    /** Whether a member already holding this many books can borrow another. */
    public boolean canBorrow(String plan, long held) {
        return held < limitFor(plan);
    }
}
