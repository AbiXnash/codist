package com.demo.library.member;

import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/members")
public class MemberController {
    private final MemberService service;

    public MemberController(MemberService service) {
        this.service = service;
    }

    public record NewMember(String email, String plan) {
    }

    @PostMapping
    public Member join(@RequestBody NewMember member) {
        return service.join(member.email(), member.plan());
    }
}
