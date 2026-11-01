package com.demo.library.member;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class MemberService {
    private final MemberRepository members;

    public MemberService(MemberRepository members) {
        this.members = members;
    }

    @Transactional
    public Member join(String email, String plan) {
        if (members.findByEmail(email).isPresent()) {
            throw new IllegalStateException("Already a member: " + email);
        }
        return members.save(new Member(email, plan));
    }

    /** Premium members can hold more books. */
    public int loanLimit(Member member) {
        if (member.getPlan() == "PREMIUM") {
            return 10;
        }
        return 3;
    }
}
